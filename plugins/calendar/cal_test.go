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
	// times are RFC3339 UTC
	if got := d0[0]["time"].(string); !strings.HasSuffix(got, "Z") {
		t.Errorf("slot time must be UTC RFC3339, got %q", got)
	}
}

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
	// TTL expiry
	h.reserve("42", start)
	time.Sleep(70 * time.Millisecond)
	if got := h.activeStarts("42"); len(got) != 0 {
		t.Fatal("expired hold must be pruned")
	}
}

func TestMeDTO_NoLeak(t *testing.T) {
	b, _ := json.Marshal(meDTO())
	// No contact fields, and no populated identity — username is deliberately empty.
	for _, forbidden := range []string{"@", `"email"`, `"name"`} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("anonymous /me leaks %q: %s", forbidden, b)
		}
	}
	if !strings.Contains(string(b), `"username":""`) {
		t.Errorf("/me should carry an empty anonymous username: %s", b)
	}
}

// --- HTTP contract tests over the real mounted mux ---

// seed boots a test app with the scheduling migration, one host with a default
// 09:00–11:00 schedule on a future weekday, and one active event type carrying a
// SECRET meeting location. Returns the plugin, host id, event slug and a valid
// future grid start (09:00 UTC).
func seed(t *testing.T) (*plugin, string, string, time.Time, func()) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	owner := "host_" + security.RandomString(6)
	day := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, 7)
	start := day.Add(9 * time.Hour)
	wd := int(start.Weekday())

	schedCol, _ := app.FindCollectionByNameOrId("availabilitySchedule")
	sched := core.NewRecord(schedCol)
	sched.Set("owner", owner)
	sched.Set("name", "default")
	sched.Set("timezone", "UTC")
	sched.Set("weekly", []map[string]any{{"weekday": wd, "startMinute": 9 * 60, "endMinute": 11 * 60}})
	sched.Set("isDefault", true)
	if err := app.Save(sched); err != nil {
		t.Fatalf("save schedule: %v", err)
	}

	etCol, _ := app.FindCollectionByNameOrId("eventType")
	et := core.NewRecord(etCol)
	et.Set("owner", owner)
	et.Set("title", "Intro Call")
	et.Set("slug", "intro")
	et.Set("description", "A quick chat")
	et.Set("durationMinutes", 30)
	et.Set("locationType", "video")
	et.Set("location", secretLocation)
	et.Set("availabilitySchedule", sched.Id)
	et.Set("active", true)
	if err := app.Save(et); err != nil {
		t.Fatalf("save eventType: %v", err)
	}

	p := &plugin{
		app:       app,
		ipLimit:   newLimiter(time.Minute, 15),
		hostLimit: newLimiter(time.Minute, 60),
		holds:     newHolds(5 * time.Minute),
	}
	return p, owner, "intro", start, app.Cleanup
}

const secretLocation = "https://meet.example.com/j/SECRET-ROOM-9f3a1c"

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
	p, owner, slug, _, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	status, body := req(t, "GET", srv.URL+"/v1/calendar/atoms/event-types/"+slug+"/public?username="+owner, "")
	if status != 200 {
		t.Fatalf("public event: want 200, got %d: %s", status, body)
	}
	for _, want := range []string{`"status":"success"`, `"bookingFields"`, `"length":30`, "Intro Call", `"locations":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("public event missing %q in: %s", want, body)
		}
	}
	// The secret meeting location must NEVER appear pre-booking.
	if strings.Contains(body, secretLocation) || strings.Contains(body, "SECRET-ROOM") {
		t.Fatalf("LEAK: public event exposed the raw meeting location: %s", body)
	}
	// No attendee/host contact fields on the public event.
	if strings.Contains(body, "attendeeEmail") || strings.Contains(body, "@") {
		t.Fatalf("LEAK: public event exposed a contact field: %s", body)
	}
}

func TestHTTP_AvailableSlots(t *testing.T) {
	p, owner, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, owner, slug))
	from := time.Now().UTC().Format(time.RFC3339)
	to := time.Now().UTC().AddDate(0, 0, 14).Format(time.RFC3339)
	url := fmt.Sprintf("%s/v1/calendar/slots/available?startTime=%s&endTime=%s&eventTypeSlug=%s&usernameList=%s&timeZone=UTC&eventTypeId=%d",
		srv.URL, from, to, slug, owner, etID)
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

func TestHTTP_ReserveHidesSlotThenDelete(t *testing.T) {
	p, owner, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, owner, slug))
	slotsURL := fmt.Sprintf("%s/v1/calendar/slots/available?startTime=%s&endTime=%s&eventTypeSlug=%s&usernameList=%s&timeZone=UTC&eventTypeId=%d",
		srv.URL, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().AddDate(0, 0, 14).Format(time.RFC3339), slug, owner, etID)

	// reserve the 09:00 slot
	body := fmt.Sprintf(`{"slotUtcStartDate":%q,"slotUtcEndDate":%q,"eventTypeId":%d}`,
		start.Format(time.RFC3339), start.Add(30*time.Minute).Format(time.RFC3339), etID)
	status, resBody := req(t, "POST", srv.URL+"/v1/calendar/slots/reserve", body)
	if status != 200 || !strings.Contains(resBody, `"status":"success"`) {
		t.Fatalf("reserve: want 200 success, got %d: %s", status, resBody)
	}
	uid := dataString(t, resBody)
	if uid == "" {
		t.Fatal("reserve must return a reservation uid")
	}

	// the held slot is now hidden from the listing
	_, slotsBody := req(t, "GET", slotsURL, "")
	if strings.Contains(slotsBody, start.Format(time.RFC3339)) {
		t.Errorf("held 09:00 slot should be hidden from listing: %s", slotsBody)
	}

	// deleting the hold brings it back
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
	p, owner, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	etID := stableNumericID(mustEventTypeID(t, p, owner, slug))
	book := fmt.Sprintf(`{"user":%q,"eventTypeSlug":%q,"eventTypeId":%d,"start":%q,"timeZone":"UTC","responses":{"name":"Ada Lovelace","email":"ada@example.com","notes":"hi"}}`,
		owner, slug, etID, start.Format(time.RFC3339))

	status, body := req(t, "POST", srv.URL+"/v1/calendar/bookings", book)
	if status != 201 {
		t.Fatalf("create booking: want 201, got %d: %s", status, body)
	}
	for _, want := range []string{`"status":"accepted"`, `"uid"`, "Ada Lovelace", "ada@example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("booking response missing %q: %s", want, body)
		}
	}
	uid := fieldString(t, body, "uid")
	if uid == "" {
		t.Fatal("booking must return a uid")
	}

	// The confirmed attendee (holding the uid) may see the real location.
	cStatus, cBody := req(t, "GET", srv.URL+"/v1/calendar/bookings/"+uid, "")
	if cStatus != 200 || !strings.Contains(cBody, secretLocation) {
		t.Fatalf("confirmation should reveal the real location to the uid holder, got %d: %s", cStatus, cBody)
	}

	// A second identical booking loses the race → 409.
	status2, body2 := req(t, "POST", srv.URL+"/v1/calendar/bookings", book)
	if status2 != 409 {
		t.Fatalf("double-book must 409, got %d: %s", status2, body2)
	}
	if !strings.Contains(body2, `"status":"error"`) {
		t.Errorf("409 must use the Cal error envelope: %s", body2)
	}
}

func TestHTTP_CrossOwnerIsolation(t *testing.T) {
	p, owner, slug, start, cleanup := seed(t)
	defer cleanup()
	srv := mux(t, p)
	defer srv.Close()

	// A different, unrelated host id must not resolve this host's event type.
	other := "host_" + security.RandomString(6)
	status, _ := req(t, "GET", srv.URL+"/v1/calendar/atoms/event-types/"+slug+"/public?username="+other, "")
	if status != 404 {
		t.Errorf("cross-owner public event must 404, got %d", status)
	}

	// A booking whose user does not own the slug must not create a booking.
	book := fmt.Sprintf(`{"user":%q,"eventTypeSlug":%q,"start":%q,"timeZone":"UTC","responses":{"name":"X","email":"x@example.com"}}`,
		other, slug, start.Format(time.RFC3339))
	bStatus, _ := req(t, "POST", srv.URL+"/v1/calendar/bookings", book)
	if bStatus != 404 {
		t.Errorf("booking under a non-owning user must 404, got %d", bStatus)
	}
	_ = owner
}

func TestHTTP_MeNoLeak(t *testing.T) {
	p, _, _, _, cleanup := seed(t)
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

func mustEventTypeID(t *testing.T, p *plugin, owner, slug string) string {
	t.Helper()
	et, err := p.eventType(owner, slug)
	if err != nil {
		t.Fatalf("event type: %v", err)
	}
	return et.Id
}

// dataString reads {"status":"success","data":"<s>"}.
func dataString(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Data string `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &env)
	return env.Data
}

// fieldString reads a top-level string field from {"status":"success","data":{...}}.
func fieldString(t *testing.T, body, field string) string {
	t.Helper()
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
