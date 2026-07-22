package calendar

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
)

// handlePublicEvent serves useAtomGetPublicEvent:
//
//	GET /v1/calendar/atoms/event-types/{slug}/public?username=<owner>
//
// It returns Cal's public-event DTO — enough for the Booker to render (title,
// length, booking fields, layouts, profile) and NOTHING that leaks the host: no
// email, no other host's types, no raw meeting location (only its type).
func (p *plugin) handlePublicEvent(e *core.RequestEvent) error {
	slug := e.Request.PathValue("slug")
	handle := firstUsername(e.Request.URL.Query().Get("username"))
	if handle == "" || slug == "" {
		return calError(e, http.StatusNotFound, "event type not found")
	}
	if !p.allowReadHandle(e, handle) {
		return calError(e, http.StatusTooManyRequests, "too many requests — slow down")
	}
	et, err := p.eventType(handle, slug)
	if err != nil {
		return calError(e, http.StatusNotFound, "event type not found")
	}
	// The internal IAM owner drives availability; it is never published.
	sched, _ := p.scheduleFor(p.app, et.GetString("owner"), et.GetString("availabilitySchedule"))
	return calOK(e, publicEventDTO(handle, et, sched))
}

// handleAvailableSlots serves useAvailableSlots:
//
//	GET /v1/calendar/slots/available?startTime=&endTime=&eventTypeSlug=&usernameList=&timeZone=&eventTypeId=
//
// The event type is resolved by (owner, slug) — the client-supplied numeric
// eventTypeId is NEVER trusted for data access, only as the advisory-hold key.
func (p *plugin) handleAvailableSlots(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	handle := usernameListParam(e)
	if handle == "" {
		return calOK(e, emptySlotsDTO())
	}
	if !p.allowReadHandle(e, handle) {
		return calError(e, http.StatusTooManyRequests, "too many requests — slow down")
	}
	et, err := p.eventType(handle, q.Get("eventTypeSlug"))
	if err != nil {
		// The Booker tolerates an empty slot map; never leak why.
		return calOK(e, emptySlotsDTO())
	}
	owner := et.GetString("owner")
	now := time.Now().UTC()
	from := parseTimeOr(q.Get("startTime"), now)
	to := parseTimeOr(q.Get("endTime"), now.AddDate(0, 0, 14))
	// Clamp the requested span BEFORE generation so a huge [startTime,endTime]
	// can't amplify one ~200-byte request into an unbounded slot computation.
	if to.Before(from) {
		to = from
	}
	if to.Sub(from) > maxSlotWindow {
		to = from.Add(maxSlotWindow)
	}
	slots, sched := p.openSlots(p.app, owner, et, from, to, now)
	if sched == nil {
		return calOK(e, emptySlotsDTO())
	}
	held := p.holds.activeStarts(q.Get("eventTypeId"))
	return calOK(e, slotsDTO(slots, held, q.Get("timeZone")))
}

// handleReserveSlot serves useReserveSlot:
//
//	POST /v1/calendar/slots/reserve {slotUtcStartDate, slotUtcEndDate, eventTypeId}
//
// It records an advisory short-TTL hold and returns its opaque uid. The hold makes
// the slot vanish from other bookers' listings while the form is filled; it does
// NOT gate the write path — the authoritative anti-double-book is the transactional
// isOpenSlot re-check plus the partial unique index in book().
func (p *plugin) handleReserveSlot(e *core.RequestEvent) error {
	if !p.allowRead(e) {
		return calError(e, http.StatusTooManyRequests, "too many requests — slow down")
	}
	var body struct {
		SlotUtcStartDate string          `json:"slotUtcStartDate"`
		SlotUtcEndDate   string          `json:"slotUtcEndDate"`
		EventTypeId      json.RawMessage `json:"eventTypeId"`
	}
	if err := e.BindBody(&body); err != nil {
		return calError(e, http.StatusBadRequest, "invalid body")
	}
	start, err := parseCalTime(body.SlotUtcStartDate)
	if err != nil {
		return calError(e, http.StatusBadRequest, "invalid slot time")
	}
	uid := p.holds.reserve(string(body.EventTypeId), start)
	return calOK(e, uid)
}

// handleDeleteSelectedSlot serves useDeleteSelectedSlot:
//
//	DELETE /v1/calendar/slots/selected-slot?uid=<holdUid>
func (p *plugin) handleDeleteSelectedSlot(e *core.RequestEvent) error {
	p.holds.release(e.Request.URL.Query().Get("uid"))
	return calOKEmpty(e)
}

// handleBookingForReschedule serves useGetBookingForReschedule:
//
//	GET /v1/calendar/bookings/{uid}/reschedule
//
// The uid is the booking's opaque capability token, so no auth is needed.
func (p *plugin) handleBookingForReschedule(e *core.RequestEvent) error {
	rec, err := p.bookingByUID(e.Request.PathValue("uid"))
	if err != nil {
		return calError(e, http.StatusNotFound, "booking not found")
	}
	return calOK(e, bookingDTO(rec))
}

// handleCreateBooking serves useCreateBooking:
//
//	POST /v1/calendar/bookings   (cal-api-version: 2024-08-13)
//
// Body is Cal's legacy BookingCreateBody (attendee under `responses`). The event
// type is resolved by (owner, slug); isOpenSlot is the single availability gate;
// book() serializes the final re-check with the write and 409s on a lost race.
func (p *plugin) handleCreateBooking(e *core.RequestEvent) error {
	var body struct {
		Start         string          `json:"start"`
		User          json.RawMessage `json:"user"`
		EventTypeSlug string          `json:"eventTypeSlug"`
		TimeZone      string          `json:"timeZone"`
		Responses     json.RawMessage `json:"responses"`
		EventTypeId   json.RawMessage `json:"eventTypeId"` // untrusted; advisory-hold key only
		Name          json.RawMessage `json:"name"`        // fallback if not nested under responses
		Email         string          `json:"email"`       // fallback
		Notes         string          `json:"notes"`       // fallback
	}
	if err := e.BindBody(&body); err != nil {
		return calError(e, http.StatusBadRequest, "invalid body")
	}
	handle := oneUser(body.User)
	if handle == "" || body.EventTypeSlug == "" {
		return calError(e, http.StatusBadRequest, "user and eventTypeSlug are required")
	}
	if !p.allow(e, "book:"+handle) {
		return calError(e, http.StatusTooManyRequests, "too many requests — try again shortly")
	}
	et, err := p.eventType(handle, body.EventTypeSlug)
	if err != nil {
		return calError(e, http.StatusNotFound, "event type not found")
	}
	owner := et.GetString("owner") // internal IAM id, from the resolved record
	start, err := parseCalTime(body.Start)
	if err != nil {
		return calError(e, http.StatusBadRequest, "invalid start time")
	}

	a := attendee{Timezone: body.TimeZone}
	// Attendee fields live under `responses` in the atom's body; accept a flat
	// top-level shape too so the handler is robust to either Cal booking version.
	if len(body.Responses) > 0 {
		var r struct {
			Name  json.RawMessage `json:"name"`
			Email string          `json:"email"`
			Notes string          `json:"notes"`
		}
		_ = json.Unmarshal(body.Responses, &r)
		a.Name = parseName(r.Name)
		a.Email = strings.TrimSpace(r.Email)
		a.Notes = r.Notes
	}
	if a.Name == "" {
		a.Name = parseName(body.Name)
	}
	if a.Email == "" {
		a.Email = strings.TrimSpace(body.Email)
	}
	if a.Notes == "" {
		a.Notes = body.Notes
	}
	if a.Name == "" || a.Email == "" {
		return calError(e, http.StatusBadRequest, "name and email are required")
	}

	// Defense in depth: reject an off-availability time before entering the write
	// transaction. book() re-checks the same gate inside the tx as the authority.
	if !p.isOpenSlot(p.app, owner, et, start) {
		return calError(e, http.StatusConflict, "that time isn't available")
	}
	rec, err := p.book(owner, et, start, a)
	if err != nil {
		return calError(e, http.StatusConflict, "that time was just taken — pick another")
	}
	// The slot is now durably booked; drop any advisory hold on it.
	p.holds.releaseStart(string(body.EventTypeId), start)
	return calOKStatus(e, http.StatusCreated, bookingDTO(rec))
}

// handleGetBooking serves the SPA confirmation view:
//
//	GET /v1/calendar/bookings/{uid}
//
// The uid is the booking's opaque capability, so this reveals the real meeting
// location (which the confirmed attendee needs) — never exposed pre-booking.
func (p *plugin) handleGetBooking(e *core.RequestEvent) error {
	rec, err := p.bookingByUID(e.Request.PathValue("uid"))
	if err != nil {
		return calError(e, http.StatusNotFound, "booking not found")
	}
	return calOK(e, bookingDTO(rec))
}

// handleCancelBooking serves the SPA cancel action:
//
//	POST /v1/calendar/bookings/{uid}/cancel {reason}
func (p *plugin) handleCancelBooking(e *core.RequestEvent) error {
	rec, err := p.bookingByUID(e.Request.PathValue("uid"))
	if err != nil {
		return calError(e, http.StatusNotFound, "booking not found")
	}
	var body struct {
		Reason             string `json:"reason"`
		CancellationReason string `json:"cancellationReason"`
	}
	_ = e.BindBody(&body)
	reason := body.Reason
	if reason == "" {
		reason = body.CancellationReason
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	rec.Set("status", "cancelled")
	rec.Set("cancelReason", reason)
	rec.Set("cancelledAt", time.Now().UTC())
	if err := p.app.Save(rec); err != nil {
		return calError(e, http.StatusInternalServerError, "failed to cancel booking")
	}
	return calOK(e, bookingDTO(rec))
}

// handleMe serves useMe. Public booking carries no access token, so this hook is
// disabled in the atom; if it is called anyway it returns an anonymous shape that
// leaks no real user — only neutral rendering preferences.
func (p *plugin) handleMe(e *core.RequestEvent) error {
	return calOK(e, meDTO())
}

// --- request parsing helpers ---

// firstUsername takes the first username from a "+"-joined usernameList.
func firstUsername(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// usernameListParam reads the slots endpoint's usernameList across the array
// serializations axios may emit (usernameList, usernameList[], usernameList[0]).
func usernameListParam(e *core.RequestEvent) string {
	q := e.Request.URL.Query()
	for _, k := range []string{"usernameList", "usernameList[]", "usernameList[0]"} {
		if v := q.Get(k); v != "" {
			return firstUsername(v)
		}
	}
	return ""
}

// oneUser extracts the host username from the booking body's `user`, which Cal
// types as string | string[].
func oneUser(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return firstUsername(s)
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		return firstUsername(arr[0])
	}
	return ""
}

// parseName reads a booking field `name`, which is either a plain string or an
// object {firstName, lastName}.
func parseName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var o struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return strings.TrimSpace(o.FirstName + " " + o.LastName)
	}
	return ""
}

// parseCalTime parses an RFC3339 instant (Cal sends both "…Z" and "…±hh:mm").
func parseCalTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// parseTimeOr parses an RFC3339 or YYYY-MM-DD time, falling back to def.
func parseTimeOr(s string, def time.Time) time.Time {
	if s == "" {
		return def
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	return def
}
