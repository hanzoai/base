package calendar

import (
	"os"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/dbx"
)

// seedHost idempotently ensures a bookable host exists when CALENDAR_SEED_HANDLE
// is set — a config-driven bootstrap so a fresh instance has a public booking
// page (handle/intro) without waiting on the host-management UI. It writes via
// app.Save (superuser context), so it bypasses the collections' owner-scoped
// rules the public API enforces. A no-op once the event type exists, so it is
// safe to run on every serve.
//
// Env: CALENDAR_SEED_HANDLE (the public handle; presence enables seeding),
// CALENDAR_SEED_OWNER (internal owner id, default = handle), CALENDAR_SEED_TZ
// (IANA tz, default America/Los_Angeles), CALENDAR_SEED_TITLE (default "Intro"),
// CALENDAR_SEED_LOCATION (default https://meet.hanzo.ai/<handle>).
func (p *plugin) seedHost() error {
	handle := os.Getenv("CALENDAR_SEED_HANDLE")
	if handle == "" {
		return nil
	}
	if _, err := p.app.FindFirstRecordByFilter("eventType",
		"handle={:handle} && slug={:slug}",
		dbx.Params{"handle": handle, "slug": "intro"}); err == nil {
		return nil // already seeded
	}

	owner := envOr("CALENDAR_SEED_OWNER", handle)
	tz := envOr("CALENDAR_SEED_TZ", "America/Los_Angeles")

	schedCol, err := p.app.FindCollectionByNameOrId("availabilitySchedule")
	if err != nil {
		return err
	}
	sched := core.NewRecord(schedCol)
	sched.Set("owner", owner)
	sched.Set("name", "Default")
	sched.Set("timezone", tz)
	sched.Set("weekly", weekdaysNineToFive())
	sched.Set("isDefault", true)
	if err := p.app.Save(sched); err != nil {
		return err
	}

	etCol, err := p.app.FindCollectionByNameOrId("eventType")
	if err != nil {
		return err
	}
	et := core.NewRecord(etCol)
	et.Set("owner", owner)
	et.Set("handle", handle)
	et.Set("slug", "intro")
	et.Set("title", envOr("CALENDAR_SEED_TITLE", "Intro"))
	et.Set("description", "30-minute intro call")
	et.Set("durationMinutes", 30)
	et.Set("locationType", "video")
	et.Set("location", envOr("CALENDAR_SEED_LOCATION", "https://meet.hanzo.ai/"+handle))
	et.Set("availabilitySchedule", sched.Id)
	et.Set("minimumNoticeMinutes", 60)
	et.Set("timezone", tz)
	et.Set("active", true)
	return p.app.Save(et)
}

// weekdaysNineToFive is a Monday–Friday 09:00–17:00 weekly availability.
func weekdaysNineToFive() []map[string]int {
	out := make([]map[string]int, 0, 5)
	for weekday := 1; weekday <= 5; weekday++ {
		out = append(out, map[string]int{"weekday": weekday, "startMinute": 540, "endMinute": 1020})
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
