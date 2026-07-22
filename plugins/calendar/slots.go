package calendar

import (
	"encoding/json"
	"time"

	"github.com/hanzoai/base/core"
)

// availWindow is one weekly availability window in the schedule's timezone.
type availWindow struct {
	Weekday     int `json:"weekday"`     // 0=Sunday … 6=Saturday
	StartMinute int `json:"startMinute"` // minutes past local midnight
	EndMinute   int `json:"endMinute"`
}

// interval is a busy span (an existing booking or a synced calendar event),
// already padded by the event type's buffers.
type interval struct{ start, end time.Time }

func (iv interval) overlaps(a, b time.Time) bool { return iv.start.Before(b) && a.Before(iv.end) }

// weeklyWindows reads a schedule's weekly availability robustly, regardless of
// how the JSON field is stored internally.
func weeklyWindows(schedule *core.Record) []availWindow {
	var out []availWindow
	raw, err := json.Marshal(schedule.Get("weekly"))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// computeSlots generates the open start times in [from,to] for an event of
// durationMin, from the weekly availability evaluated in loc, dropping slots that
// overlap a busy interval or fall inside the minimum-notice horizon. Pure — no DB.
func computeSlots(from, to, now time.Time, durationMin, minNoticeMin int, windows []availWindow, loc *time.Location, busy []interval) []time.Time {
	if durationMin <= 0 || len(windows) == 0 {
		return nil
	}
	var slots []time.Time
	dur := time.Duration(durationMin) * time.Minute
	notBefore := now.Add(time.Duration(minNoticeMin) * time.Minute)

	f := from.In(loc)
	day := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, loc)
	end := to.In(loc)
	for !day.After(end) {
		wd := int(day.Weekday())
		for _, w := range windows {
			if w.Weekday != wd {
				continue
			}
			for m := w.StartMinute; m+durationMin <= w.EndMinute; m += durationMin {
				start := day.Add(time.Duration(m) * time.Minute)
				slotEnd := start.Add(dur)
				su := start.UTC()
				if su.Before(from) || su.After(to) || su.Before(notBefore) {
					continue
				}
				open := true
				for _, iv := range busy {
					if iv.overlaps(start, slotEnd) {
						open = false
						break
					}
				}
				if open {
					slots = append(slots, su)
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return slots
}
