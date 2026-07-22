package calendar

import (
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/security"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

// eventType loads an active event type by its host and slug. The query is owner-
// and slug-scoped and active-only, so it never enumerates another host's types.
func (p *plugin) eventType(owner, slug string) (*core.Record, error) {
	return p.app.FindFirstRecordByFilter("eventType",
		"owner={:owner} && slug={:slug} && active=true",
		dbx.Params{"owner": owner, "slug": slug})
}

func (p *plugin) bookingByUID(uid string) (*core.Record, error) {
	return p.app.FindFirstRecordByFilter("booking", "uid={:uid}", dbx.Params{"uid": uid})
}

// scheduleFor resolves the availability schedule for an event type — the one it
// references, else the host's default.
func (p *plugin) scheduleFor(app core.App, owner, id string) (*core.Record, error) {
	if id != "" {
		if rec, err := app.FindRecordById("availabilitySchedule", id); err == nil {
			return rec, nil
		}
	}
	return app.FindFirstRecordByFilter("availabilitySchedule",
		"owner={:owner} && isDefault=true", dbx.Params{"owner": owner})
}

// openSlots computes the host's open start times for the event type in [from,to],
// evaluated in the schedule's timezone, minus existing bookings and synced calendar
// events (padded by buffers). It is the one availability computation both the slots
// listing and the write gate consult. Returns the slots and the schedule record.
func (p *plugin) openSlots(app core.App, owner string, et *core.Record, from, to, now time.Time) ([]time.Time, *core.Record) {
	sched, err := p.scheduleFor(app, owner, et.GetString("availabilitySchedule"))
	if err != nil {
		return nil, nil
	}
	loc := loadLoc(sched.GetString("timezone"))
	busy := p.busyIntervals(app, owner, from, to, et.GetInt("bufferBeforeMinutes"), et.GetInt("bufferAfterMinutes"))
	slots := computeSlots(from, to, now, et.GetInt("durationMinutes"), et.GetInt("minimumNoticeMinutes"), weeklyWindows(sched), loc, busy)
	return slots, sched
}

// isOpenSlot reports whether start is a genuinely bookable slot for the event type
// against the given app (which may be a transaction): inside the host's
// availability, past the minimum-notice horizon, future-dated, grid-aligned and
// conflict-free (incl. buffers). It is the single source of truth reused by both
// the slots listing and the booking write, so the write path can never accept a
// time the listing wouldn't render. Advisory holds deliberately do NOT gate this —
// the booker whose hold it is must still be able to book their held slot.
func (p *plugin) isOpenSlot(app core.App, owner string, et *core.Record, start time.Time) bool {
	from := start.Add(-24 * time.Hour)
	to := start.Add(24 * time.Hour)
	slots, _ := p.openSlots(app, owner, et, from, to, time.Now().UTC())
	for _, s := range slots {
		if s.Equal(start) {
			return true
		}
	}
	return false
}

// busyIntervals collects the host's non-cancelled bookings and synced calendar
// events that OVERLAP [from,to], padded by the event type's buffers. The overlap
// is filtered in SQL (startsAt < to && endsAt > from) so a host with many rows
// never blinds the conflict check the way a fixed row cap would.
func (p *plugin) busyIntervals(app core.App, owner string, from, to time.Time, bufBefore, bufAfter int) []interval {
	var out []interval
	fromDT, _ := types.ParseDateTime(from)
	toDT, _ := types.ParseDateTime(to)
	params := dbx.Params{"owner": owner, "from": fromDT, "to": toDT}
	add := func(s, en time.Time) {
		s = s.Add(-time.Duration(bufBefore) * time.Minute)
		en = en.Add(time.Duration(bufAfter) * time.Minute)
		out = append(out, interval{s, en})
	}
	if recs, err := app.FindRecordsByFilter("booking",
		"owner={:owner} && status!='cancelled' && startsAt<{:to} && endsAt>{:from}",
		"startsAt", 2000, 0, params); err == nil {
		for _, r := range recs {
			add(r.GetDateTime("startsAt").Time(), r.GetDateTime("endsAt").Time())
		}
	}
	if recs, err := app.FindRecordsByFilter("calendarEvent",
		"owner={:owner} && isCanceled=false && startsAt<{:to} && endsAt>{:from}",
		"startsAt", 2000, 0, params); err == nil {
		for _, r := range recs {
			add(r.GetDateTime("startsAt").Time(), r.GetDateTime("endsAt").Time())
		}
	}
	return out
}

// attendee carries the validated primary-attendee input from a booking request.
type attendee struct {
	Name, Email, Timezone, Notes string
}

// book creates a booking for the event type at start, serializing the final
// availability re-check with the write in one transaction. The partial unique
// index on (owner, startsAt) is the backstop against a concurrent double-book — its
// violation surfaces here as an error. Returns the saved record or errSlotTaken.
func (p *plugin) book(owner string, et *core.Record, start time.Time, a attendee) (*core.Record, error) {
	col, err := p.app.FindCollectionByNameOrId("booking")
	if err != nil {
		return nil, err
	}
	end := start.Add(time.Duration(et.GetInt("durationMinutes")) * time.Minute)
	rec := core.NewRecord(col)
	rec.Set("owner", owner)
	rec.Set("eventType", et.Id)
	rec.Set("startsAt", start)
	rec.Set("endsAt", end)
	rec.Set("timezone", a.Timezone)
	rec.Set("status", "accepted")
	rec.Set("title", et.GetString("title"))
	rec.Set("location", et.GetString("location"))
	rec.Set("attendeeName", a.Name)
	rec.Set("attendeeEmail", a.Email)
	rec.Set("attendeeTimezone", a.Timezone)
	rec.Set("attendeeNotes", a.Notes)
	rec.Set("uid", security.RandomString(24))

	err = p.app.RunInTransaction(func(txApp core.App) error {
		if !p.isOpenSlot(txApp, owner, et, start) {
			return errSlotTaken
		}
		return txApp.Save(rec)
	})
	if err != nil {
		return nil, errSlotTaken
	}
	return rec, nil
}

func loadLoc(tz string) *time.Location {
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}
