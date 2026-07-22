package scheduling

import (
	"net/http"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/security"
	"github.com/hanzoai/dbx"
)

// eventType loads an active, public event type by its host and slug.
func (p *plugin) eventType(owner, slug string) (*core.Record, error) {
	return p.app.FindFirstRecordByFilter("eventType",
		"owner={:owner} && slug={:slug} && active=true",
		dbx.Params{"owner": owner, "slug": slug})
}

func eventTypeJSON(rec *core.Record) map[string]any {
	return map[string]any{
		"owner":           rec.GetString("owner"),
		"slug":            rec.GetString("slug"),
		"title":           rec.GetString("title"),
		"description":     rec.GetString("description"),
		"durationMinutes": rec.GetInt("durationMinutes"),
		"locationType":    rec.GetString("locationType"),
		"timezone":        rec.GetString("timezone"),
	}
}

func bookingJSON(rec *core.Record) map[string]any {
	return map[string]any{
		"uid":           rec.GetString("uid"),
		"status":        rec.GetString("status"),
		"title":         rec.GetString("title"),
		"startsAt":      rec.GetDateTime("startsAt").Time().UTC().Format(time.RFC3339),
		"endsAt":        rec.GetDateTime("endsAt").Time().UTC().Format(time.RFC3339),
		"attendeeName":  rec.GetString("attendeeName"),
		"attendeeEmail": rec.GetString("attendeeEmail"),
		"location":      rec.GetString("location"),
	}
}

// GET /v1/schedule/{owner}/{slug}
func (p *plugin) handleGetEventType(e *core.RequestEvent) error {
	rec, err := p.eventType(e.Request.PathValue("owner"), e.Request.PathValue("slug"))
	if err != nil {
		return e.NotFoundError("event type not found", err)
	}
	return e.JSON(http.StatusOK, eventTypeJSON(rec))
}

// GET /v1/schedule/{owner}/{slug}/slots?from=&to=
func (p *plugin) handleGetSlots(e *core.RequestEvent) error {
	owner := e.Request.PathValue("owner")
	et, err := p.eventType(owner, e.Request.PathValue("slug"))
	if err != nil {
		return e.NotFoundError("event type not found", err)
	}
	sched, err := p.scheduleFor(owner, et.GetString("availabilitySchedule"))
	if err != nil {
		return e.JSON(http.StatusOK, map[string]any{"slots": []string{}})
	}
	loc, err := time.LoadLocation(sched.GetString("timezone"))
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().UTC()
	from := parseTimeOr(e.Request.URL.Query().Get("from"), now)
	to := parseTimeOr(e.Request.URL.Query().Get("to"), now.AddDate(0, 0, 14))
	busy := p.busyIntervals(owner, from, to, et.GetInt("bufferBeforeMinutes"), et.GetInt("bufferAfterMinutes"))
	slots := computeSlots(from, to, now, et.GetInt("durationMinutes"), et.GetInt("minimumNoticeMinutes"), weeklyWindows(sched), loc, busy)

	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.Format(time.RFC3339))
	}
	return e.JSON(http.StatusOK, map[string]any{
		"slots":           out,
		"durationMinutes": et.GetInt("durationMinutes"),
		"timezone":        sched.GetString("timezone"),
	})
}

// POST /v1/schedule/{owner}/{slug}/book
func (p *plugin) handleBook(e *core.RequestEvent) error {
	owner := e.Request.PathValue("owner")
	et, err := p.eventType(owner, e.Request.PathValue("slug"))
	if err != nil {
		return e.NotFoundError("event type not found", err)
	}
	var body struct {
		Start, AttendeeName, AttendeeEmail, Timezone, Notes string
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid body", err)
	}
	if body.Start == "" || body.AttendeeEmail == "" {
		return e.BadRequestError("start and attendeeEmail are required", nil)
	}
	start, err := time.Parse(time.RFC3339, body.Start)
	if err != nil {
		return e.BadRequestError("invalid start time", err)
	}
	start = start.UTC()
	end := start.Add(time.Duration(et.GetInt("durationMinutes")) * time.Minute)

	// Re-check the slot is still free before committing.
	for _, iv := range p.busyIntervals(owner, start.Add(-24*time.Hour), end.Add(24*time.Hour), 0, 0) {
		if iv.overlaps(start, end) {
			return e.JSON(http.StatusConflict, map[string]any{"error": "slot is no longer available"})
		}
	}

	col, err := p.app.FindCollectionByNameOrId("booking")
	if err != nil {
		return e.InternalServerError("scheduling not initialized", err)
	}
	rec := core.NewRecord(col)
	rec.Set("owner", owner)
	rec.Set("eventType", et.Id)
	rec.Set("startsAt", start)
	rec.Set("endsAt", end)
	rec.Set("timezone", body.Timezone)
	rec.Set("status", "accepted")
	rec.Set("title", et.GetString("title"))
	rec.Set("location", et.GetString("location"))
	rec.Set("attendeeName", body.AttendeeName)
	rec.Set("attendeeEmail", body.AttendeeEmail)
	rec.Set("attendeeTimezone", body.Timezone)
	rec.Set("attendeeNotes", body.Notes)
	rec.Set("uid", security.RandomString(24))
	if err := p.app.Save(rec); err != nil {
		return e.InternalServerError("failed to create booking", err)
	}
	return e.JSON(http.StatusCreated, bookingJSON(rec))
}

// GET /v1/booking/{uid}
func (p *plugin) handleGetBooking(e *core.RequestEvent) error {
	rec, err := p.bookingByUID(e.Request.PathValue("uid"))
	if err != nil {
		return e.NotFoundError("booking not found", err)
	}
	return e.JSON(http.StatusOK, bookingJSON(rec))
}

// POST /v1/booking/{uid}/cancel
func (p *plugin) handleCancelBooking(e *core.RequestEvent) error {
	rec, err := p.bookingByUID(e.Request.PathValue("uid"))
	if err != nil {
		return e.NotFoundError("booking not found", err)
	}
	var body struct{ Reason string }
	_ = e.BindBody(&body)
	rec.Set("status", "cancelled")
	rec.Set("cancelReason", body.Reason)
	rec.Set("cancelledAt", time.Now().UTC())
	if err := p.app.Save(rec); err != nil {
		return e.InternalServerError("failed to cancel booking", err)
	}
	return e.JSON(http.StatusOK, bookingJSON(rec))
}

func (p *plugin) bookingByUID(uid string) (*core.Record, error) {
	return p.app.FindFirstRecordByFilter("booking", "uid={:uid}", dbx.Params{"uid": uid})
}

// scheduleFor resolves the availability schedule for an event type — the one it
// references, else the host's default.
func (p *plugin) scheduleFor(owner, id string) (*core.Record, error) {
	if id != "" {
		if rec, err := p.app.FindRecordById("availabilitySchedule", id); err == nil {
			return rec, nil
		}
	}
	return p.app.FindFirstRecordByFilter("availabilitySchedule",
		"owner={:owner} && isDefault=true", dbx.Params{"owner": owner})
}

// busyIntervals collects the host's non-cancelled bookings and synced calendar
// events overlapping [from,to], padded by the event type's buffers.
func (p *plugin) busyIntervals(owner string, from, to time.Time, bufBefore, bufAfter int) []interval {
	var out []interval
	add := func(s, en time.Time) {
		s = s.Add(-time.Duration(bufBefore) * time.Minute)
		en = en.Add(time.Duration(bufAfter) * time.Minute)
		if en.After(from) && s.Before(to) {
			out = append(out, interval{s, en})
		}
	}
	if recs, err := p.app.FindRecordsByFilter("booking",
		"owner={:owner} && status!='cancelled'", "startsAt", 1000, 0, dbx.Params{"owner": owner}); err == nil {
		for _, r := range recs {
			add(r.GetDateTime("startsAt").Time(), r.GetDateTime("endsAt").Time())
		}
	}
	if recs, err := p.app.FindRecordsByFilter("calendarEvent",
		"owner={:owner} && isCanceled=false", "startsAt", 1000, 0, dbx.Params{"owner": owner}); err == nil {
		for _, r := range recs {
			add(r.GetDateTime("startsAt").Time(), r.GetDateTime("endsAt").Time())
		}
	}
	return out
}

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
