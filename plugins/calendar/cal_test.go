package calendar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/security"
)

// --- pure DTO / adapter tests ---

func TestStableNumericID(t *testing.T) {
	a := stableNumericID("abc123")
	if a <= 0 {
		t.Fatalf("id must be positive, got %d", a)
	}
	if a != stableNumericID("abc123") {
		t.Fatal("id must be deterministic")
	}
	if stableNumericID("abc123") == stableNumericID("abc124") {
		t.Fatal("distinct ids should (almost always) differ")
	}
}

func TestSlotsDTO_GroupingAndHolds(t *testing.T) {
	day := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	nine := day.Add(9 * time.Hour)
	tenNextDay := day.Add(34 * time.Hour) // next day 10:00
	slots := []time.Time{nine, day.Add(9*time.Hour + 30*time.Minute), tenNextDay}

	// hold the 09:00 slot — it must vanish from the listing.
	out := slotsDTO(slots, []time.Time{nine}, "UTC")
	grouped := out["slots"].(map[string]any)
	d0 := grouped["2026-07-27"].([]map[string]any)
	if len(d0) != 1 {
		t.Fatalf("held 09:00 should be dropped, leaving 1 on day0, got %d", len(d0))
	}
	if grouped["2026-07-28"] == nil {
		t.Fatal("next-day slot should group under its own date")
	}
	if got := d0[0]["time"].(string); !strings.HasSuffix(got, "Z") {
		t.Errorf("slot time must be UTC RFC3339, got %q", got)
	}
}

// TestComputeSlotsCapBounds proves the maxSlots cap stops generation early, so a
// wide window cannot produce an unbounded slice (Red PoC #1, unit level).
func TestComputeSlotsCapBounds(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(5, 0, 0) // 5 years
	now := from.AddDate(-1, 0, 0)
	allDay := make([]availWindow, 0, 7)
	for wd := 0; wd < 7; wd++ {
		allDay = append(allDay, availWindow{Weekday: wd, StartMinute: 0, EndMinute: 24 * 60})
	}
	uncapped := computeSlots(from, to, now, 10, 0, allDay, loc, nil, 0)
	capped := computeSlots(from, to, now, 10, 0, allDay, loc, nil, maxSlotsPerQuery)
	if len(capped) != maxSlotsPerQuery {
		t.Fatalf("capped generation should stop at %d, got %d", maxSlotsPerQuery, len(capped))
	}
	if len(uncapped) <= maxSlotsPerQuery {
		t.Fatalf("sanity: the 5-year window should generate far more than the cap, got %d", len(uncapped))
	}
	t.Logf("cap bounds generation: uncapped=%d -> capped=%d", len(uncapped), len(capped))
}

func TestMeDTO_NoLeak(t *testing.T) {
	b, _ := json.Marshal(meDTO())
	for _, forbidden := range []string{"@", `"email"`, `"name"`} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("anonymous /me leaks %q: %s", forbidden, b)
		}
	}
	if !strings.Contains(string(b), `"username":""`) {
		t.Errorf("/me should carry an empty anonymous username: %s", b)
	}
}

// TestHolds_Lifecycle — reserve/active/release/expire.
func TestHolds_Lifecycle(t *testing.T) {
	h := newHolds(50 * time.Millisecond)
	start := time.Now().Truncate(time.Hour).Add(48 * time.Hour)
	uid := h.reserve("42", start)
	if got := h.activeStarts("42"); len(got) != 1 || !got[0].Equal(start.UTC()) {
		t.Fatalf("reserved slot must be active, got %v", got)
	}
	if got := h.activeStarts("99"); len(got) != 0 {
		t.Fatal("hold must be scoped to its eventTypeId")
	}
	h.release(uid)
	if got := h.activeStarts("42"); len(got) != 0 {
		t.Fatal("released hold must be gone")
	}
	h.reserve("42", start)
	time.Sleep(70 * time.Millisecond)
	if got := h.activeStarts("42"); len(got) != 0 {
		t.Fatal("expired hold must be pruned")
	}
}

// TestHolds_HardCap reproduces Red PoC #2 (reserve spam) and asserts the map is
// HARD-bounded — fresh reserves past the TTL no longer grow it without limit.
func TestHolds_HardCap(t *testing.T) {
	h := newHolds(time.Hour) // long TTL: nothing expires during the test
	start := time.Now().Add(48 * time.Hour)
	spam := holdsMaxEntries + 5000
	for i := 0; i < spam; i++ {
		h.reserve(fmt.Sprintf("et%d", i), start.Add(time.Duration(i)*time.Minute))
	}
	if len(h.byUID) > holdsMaxEntries {
		t.Fatalf("holds map exceeded hard cap: %d > %d", len(h.byUID), holdsMaxEntries)
	}
	t.Logf("holds hard-capped at %d after %d fresh reserves (before fix: retained all %d)", len(h.byUID), spam, spam)
}

// TestLimiter_HardCap reproduces Red PoC #3 (key rotation) and asserts the limiter
// map is HARD-bounded — rotating distinct keys no longer grows it without limit.
func TestLimiter_HardCap(t *testing.T) {
	l := newLimiter(time.Hour, 5) // long window: nothing expires
	now := time.Now()
	spam := limiterMaxKeys + 5000
	for i := 0; i < spam; i++ {
		l.allow(fmt.Sprintf("k%d", i), now)
	}
	if len(l.hits) > limiterMaxKeys {
		t.Fatalf("limiter map exceeded hard cap: %d > %d", len(l.hits), limiterMaxKeys)
	}
	t.Logf("limiter hard-capped at %d after %d distinct keys (before fix: retained all %d)", len(l.hits), spam, spam)
}

// --- HTTP contract tests over the real mounted mux ---

const secretLocation = "https://meet.example.com/j/SECRET-ROOM-9f3a1c"

func nextWeekNine() time.Time {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 7).Add(9 * time.Hour)
}

// seedWith boots a test app and seeds one host with the given weekly availability
// and event duration. owner is a distinct INTERNAL IAM id that must never surface;
// handle is the PUBLIC booking identifier used in URLs.
func seedWith(t *testing.T, weekly []map[string]any, durationMin int) (p *plugin, handle, owner, slug string, start time.Time, cleanup func()) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	owner = "iamsub_" + security.RandomString(12) // internal IAM subject — never published
	handle = "host" + security.RandomString(6)    // public booking handle
	slug = "intro"
	start = nextWeekNine()

	schedCol, _ := app.FindCollectionByNameOrId("availabilitySchedule")
	sched := core.NewRecord(schedCol)
	sched.Set("owner", owner)
	sched.Set("name", "default")
	sched.Set("timezone", "UTC")
	sched.Set("weekly", weekly)
	sched.Set("isDefault", true)
	if err := app.Save(sched); err != nil {
		t.Fatalf("save schedule: %v", err)
	}

	etCol, _ := app.FindCollectionByNameOrId("eventType")
	et := core.NewRecord(etCol)
	et.Set("owner", owner)
	et.Set("handle", handle)
	et.Set("title", "Intro Call")
	et.Set("slug", slug)
	et.Set("description", "A quick chat")
	et.Set("durationMinutes", durationMin)
	et.Set("locationType", "video")
	et.Set("location", secretLocation)
	et.Set("availabilitySchedule", sched.Id)
	et.Set("active", true)
	if err := app.Save(et); err != nil {
		t.Fatalf("save eventType: %v", err)
	}

	p = &plugin{
		app:             app,
		ipLimit:         newLimiter(time.Minute, 15),
		hostLimit:       newLimiter(time.Minute, 60),
		readIPLimit:     newLimiter(time.Minute, 240),
		readHandleLimit: newLimiter(time.Minute, 600),
		holds:           newHolds(5 * time.Minute),
	}
	return p, handle, owner, slug, start, app.Cleanup
}

// seed is the standard fixture: 09:00–11:00 on the (fixed, future) start weekday,
// 30-minute events.
func seed(t *testing.T) (p *plugin, handle, owner, slug string, start time.Time, cleanup func()) {
	t.Helper()
	wd := int(nextWeekNine().Weekday())
	return seedWith(t, []map[string]any{{"weekday": wd, "startMinute": 9 * 60, "endMinute": 11 * 60}}, 30)
}

func mux(t *testing.T, p *plugin) *httptest.Server {
	t.Helper()
	r, err := apis.NewRouter(p.app)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	p.registerRoutes(r)
	m, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	return httptest.NewServer(m)
}

func req(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rq, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	rq.Header.Set("Content-Type", "application/json")
	rq.Header.Set("cal-api-version", "2024-08-13")
	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(b)
}

func TestHTTP_PublicEventNoLeak(t *testing.T) {
	p, handle, owner, slug, _, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	status, body := req(t, "GET", srv.URL+"/v1/calendar/atoms/event-types/"+slug+"/public?username="+handle, "")
	if status != 200 {
		t.Fatalf("public event: want 200, got %d: %s", status, body)
	}
	for _, want := range []string{`"status":"success"`, `"bookingFields"`, `"length":30`, "Intro Call", `"locations":[]`, `"username":"` + handle} {
		if !strings.Contains(body, want) {
			t.Errorf("public event missing %q in: %s", want, body)
		}
	}
	// The raw IAM owner id must NEVER be published (decoupled from the public handle).
	if strings.Contains(body, owner) {
		t.Fatalf("LEAK: public event exposed the internal IAM owner id %q: %s", owner, body)
	}
	// The secret meeting location must NEVER appear pre-booking.
	if strings.Contains(body, secretLocation) || strings.Contains(body, "SECRET-ROOM") {
		t.Fatalf("LEAK: public event exposed the raw meeting location: %s", body)
	}
	if strings.Contains(body, "attendeeEmail") || strings.Contains(body, "@") {
		t.Fatalf("LEAK: public event exposed a contact field: %s", body)
	}
}

func TestHTTP_AvailableSlots(t *testing.T) {
	p, handle, _, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, handle, slug))
	from := time.Now().UTC().Format(time.RFC3339)
	to := time.Now().UTC().AddDate(0, 0, 14).Format(time.RFC3339)
	url := fmt.Sprintf("%s/v1/calendar/slots/available?startTime=%s&endTime=%s&eventTypeSlug=%s&usernameList=%s&timeZone=UTC&eventTypeId=%d",
		srv.URL, from, to, slug, handle, etID)
	status, body := req(t, "GET", url, "")
	if status != 200 {
		t.Fatalf("slots: want 200, got %d: %s", status, body)
	}
	dayKey := start.Format("2006-01-02")
	if !strings.Contains(body, `"status":"success"`) || !strings.Contains(body, dayKey) {
		t.Fatalf("slots missing success/day %q: %s", dayKey, body)
	}
	if !strings.Contains(body, start.Format(time.RFC3339)) {
		t.Errorf("slots should include the 09:00 start %s: %s", start.Format(time.RFC3339), body)
	}
}

// TestHTTP_SlotsAmplificationBounded reproduces Red PoC #1: a ~1000-year range from a
// ~200-byte request. It must be clamped (≤ ~62 days) and capped (≤ maxSlotsPerQuery),
// not the measured 203,164 slots / 7.26 MB.
func TestHTTP_SlotsAmplificationBounded(t *testing.T) {
	allWeek := make([]map[string]any, 0, 7)
	for wd := 0; wd < 7; wd++ {
		allWeek = append(allWeek, map[string]any{"weekday": wd, "startMinute": 0, "endMinute": 24 * 60})
	}
	p, handle, _, slug, _, cleanup := seedWith(t, allWeek, 10) // dense: all-day, all-week, 10-min
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, handle, slug))
	// startTime=now so the clamped window covers real future slots; endTime=year 3000.
	url := fmt.Sprintf("%s/v1/calendar/slots/available?startTime=%s&endTime=3000-01-01T00:00:00Z&eventTypeSlug=%s&usernameList=%s&timeZone=UTC&eventTypeId=%d",
		srv.URL, time.Now().UTC().Format(time.RFC3339), slug, handle, etID)
	status, body := req(t, "GET", url, "")
	if status != 200 {
		t.Fatalf("slots: want 200, got %d (len %d)", status, len(body))
	}
	var env struct {
		Data struct {
			Slots map[string][]map[string]any `json:"slots"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total := 0
	for _, v := range env.Data.Slots {
		total += len(v)
	}
	days := len(env.Data.Slots)
	if total > maxSlotsPerQuery {
		t.Fatalf("amplification NOT bounded: %d slots > cap %d", total, maxSlotsPerQuery)
	}
	if total != maxSlotsPerQuery {
		t.Fatalf("dense schedule over the clamped window should hit the cap exactly, got %d (want %d)", total, maxSlotsPerQuery)
	}
	if days > 63 {
		t.Fatalf("window NOT clamped: %d days > ~62-day clamp", days)
	}
	t.Logf("amplification bounded: 1000-year range -> %d slots / %d bytes across %d days (cap %d; unclamped Red PoC was 203,164 slots / 7.26 MB)",
		total, len(body), days, maxSlotsPerQuery)
}

func TestHTTP_ReserveHidesSlotThenDelete(t *testing.T) {
	p, handle, _, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, handle, slug))
	slotsURL := fmt.Sprintf("%s/v1/calendar/slots/available?startTime=%s&endTime=%s&eventTypeSlug=%s&usernameList=%s&timeZone=UTC&eventTypeId=%d",
		srv.URL, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().AddDate(0, 0, 14).Format(time.RFC3339), slug, handle, etID)

	body := fmt.Sprintf(`{"slotUtcStartDate":%q,"slotUtcEndDate":%q,"eventTypeId":%d}`,
		start.Format(time.RFC3339), start.Add(30*time.Minute).Format(time.RFC3339), etID)
	status, resBody := req(t, "POST", srv.URL+"/v1/calendar/slots/reserve", body)
	if status != 200 || !strings.Contains(resBody, `"status":"success"`) {
		t.Fatalf("reserve: want 200 success, got %d: %s", status, resBody)
	}
	uid := dataString(resBody)
	if uid == "" {
		t.Fatal("reserve must return a reservation uid")
	}

	_, slotsBody := req(t, "GET", slotsURL, "")
	if strings.Contains(slotsBody, start.Format(time.RFC3339)) {
		t.Errorf("held 09:00 slot should be hidden from listing: %s", slotsBody)
	}

	dStatus, _ := req(t, "DELETE", srv.URL+"/v1/calendar/slots/selected-slot?uid="+uid, "")
	if dStatus != 200 {
		t.Fatalf("delete selected-slot: want 200, got %d", dStatus)
	}
	_, slotsBody2 := req(t, "GET", slotsURL, "")
	if !strings.Contains(slotsBody2, start.Format(time.RFC3339)) {
		t.Errorf("released slot should return to the listing: %s", slotsBody2)
	}
}

func TestHTTP_CreateBookingRevealsLocationAndBlocksDoubleBook(t *testing.T) {
	p, handle, owner, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, handle, slug))
	book := fmt.Sprintf(`{"user":%q,"eventTypeSlug":%q,"eventTypeId":%d,"start":%q,"timeZone":"UTC","responses":{"name":"Ada Lovelace","email":"ada@example.com","notes":"hi"}}`,
		handle, slug, etID, start.Format(time.RFC3339))

	status, body := req(t, "POST", srv.URL+"/v1/calendar/bookings", book)
	if status != 201 {
		t.Fatalf("create booking: want 201, got %d: %s", status, body)
	}
	for _, want := range []string{`"status":"accepted"`, `"uid"`, "Ada Lovelace", "ada@example.com", `"username":"` + handle} {
		if !strings.Contains(body, want) {
			t.Errorf("booking response missing %q: %s", want, body)
		}
	}
	// The internal IAM owner id must not appear in the booking response either.
	if strings.Contains(body, owner) {
		t.Fatalf("LEAK: booking response exposed the internal IAM owner id %q: %s", owner, body)
	}
	uid := fieldString(body, "uid")
	if uid == "" {
		t.Fatal("booking must return a uid")
	}

	cStatus, cBody := req(t, "GET", srv.URL+"/v1/calendar/bookings/"+uid, "")
	if cStatus != 200 || !strings.Contains(cBody, secretLocation) {
		t.Fatalf("confirmation should reveal the real location to the uid holder, got %d: %s", cStatus, cBody)
	}

	status2, body2 := req(t, "POST", srv.URL+"/v1/calendar/bookings", book)
	if status2 != 409 {
		t.Fatalf("double-book must 409, got %d: %s", status2, body2)
	}
	if !strings.Contains(body2, `"status":"error"`) {
		t.Errorf("409 must use the Cal error envelope: %s", body2)
	}
}

func TestHTTP_CrossOwnerIsolation(t *testing.T) {
	p, handle, _, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	other := "host" + security.RandomString(6)
	status, _ := req(t, "GET", srv.URL+"/v1/calendar/atoms/event-types/"+slug+"/public?username="+other, "")
	if status != 404 {
		t.Errorf("cross-handle public event must 404, got %d", status)
	}

	book := fmt.Sprintf(`{"user":%q,"eventTypeSlug":%q,"start":%q,"timeZone":"UTC","responses":{"name":"X","email":"x@example.com"}}`,
		other, slug, start.Format(time.RFC3339))
	bStatus, _ := req(t, "POST", srv.URL+"/v1/calendar/bookings", book)
	if bStatus != 404 {
		t.Errorf("booking under a non-owning handle must 404, got %d", bStatus)
	}
	_ = handle
}

// TestHTTP_ReadRateLimited proves the public reserve path is throttled per IP (Red's
// missing-read-limit finding): a single IP exceeding the budget eventually gets 429.
func TestHTTP_ReadRateLimited(t *testing.T) {
	p, handle, _, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, handle, slug))
	body := fmt.Sprintf(`{"slotUtcStartDate":%q,"slotUtcEndDate":%q,"eventTypeId":%d}`,
		start.Format(time.RFC3339), start.Add(30*time.Minute).Format(time.RFC3339), etID)

	sent := 0
	got429 := false
	for i := 0; i < 245 && !got429; i++ {
		rq, _ := http.NewRequest("POST", srv.URL+"/v1/calendar/slots/reserve", strings.NewReader(body))
		rq.Header.Set("Content-Type", "application/json")
		rq.Header.Set("X-Forwarded-For", "203.0.113.7") // fixed client IP
		resp, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		resp.Body.Close()
		sent++
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
		}
	}
	if !got429 {
		t.Fatalf("expected a 429 within 245 reserves from one IP (budget 240/min), got none")
	}
	t.Logf("read path throttled: 429 after %d reserves from one IP (budget 240/min)", sent)
}

func TestHTTP_MeNoLeak(t *testing.T) {
	p, _, _, _, _, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	status, body := req(t, "GET", srv.URL+"/v1/calendar/me", "")
	if status != 200 {
		t.Fatalf("me: want 200, got %d", status)
	}
	if strings.Contains(body, "@") {
		t.Errorf("/me must not leak any email: %s", body)
	}
}

// --- test helpers ---

func mustEventTypeID(t *testing.T, p *plugin, handle, slug string) string {
	t.Helper()
	et, err := p.eventType(handle, slug)
	if err != nil {
		t.Fatalf("event type: %v", err)
	}
	return et.Id
}

func dataString(body string) string {
	var env struct {
		Data string `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &env)
	return env.Data
}

func fieldString(body, field string) string {
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return ""
	}
	if v, ok := env.Data[field].(string); ok {
		return v
	}
	return ""
}
