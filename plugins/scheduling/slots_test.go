package scheduling

import (
	"testing"
	"time"
)

func TestComputeSlots(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 7, 27, 0, 0, 0, 0, loc)
	wd := int(from.Weekday())
	to := from.Add(24 * time.Hour)
	now := from.Add(-48 * time.Hour) // well before → minimum-notice never filters
	windows := []availWindow{{Weekday: wd, StartMinute: 9 * 60, EndMinute: 11 * 60}}

	// 9:00–11:00, 30-minute event, nothing busy → 9:00, 9:30, 10:00, 10:30.
	got := computeSlots(from, to, now, 30, 0, windows, loc, nil)
	if len(got) != 4 {
		t.Fatalf("want 4 slots, got %d (%v)", len(got), got)
	}
	if got[0] != from.Add(9*time.Hour) {
		t.Errorf("first slot = %v, want 09:00", got[0])
	}

	// A busy 10:00–10:30 interval drops exactly the 10:00 slot.
	busy := []interval{{start: from.Add(10 * time.Hour), end: from.Add(10*time.Hour + 30*time.Minute)}}
	got = computeSlots(from, to, now, 30, 0, windows, loc, busy)
	if len(got) != 3 {
		t.Fatalf("with one conflict want 3 slots, got %d (%v)", len(got), got)
	}
	for _, s := range got {
		if s.Equal(from.Add(10 * time.Hour)) {
			t.Errorf("10:00 slot should have been removed by the conflict")
		}
	}

	// Minimum notice of 12h removes the morning slots (now is 48h before, so notice
	// horizon = now+12h, still before 09:00 → keeps them). Push now to the same day.
	sameDayNow := from.Add(9*time.Hour + 45*time.Minute) // 09:45 that day
	got = computeSlots(from, to, sameDayNow, 30, 0, windows, loc, nil)
	// only 10:00 and 10:30 remain (>= 09:45).
	if len(got) != 2 {
		t.Fatalf("with now=09:45 want 2 future slots, got %d (%v)", len(got), got)
	}
}

func TestComputeSlotsGuards(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 7, 27, 0, 0, 0, 0, loc)
	to := from.Add(24 * time.Hour)
	if s := computeSlots(from, to, from, 0, 0, []availWindow{{Weekday: 1, StartMinute: 0, EndMinute: 600}}, loc, nil); s != nil {
		t.Errorf("zero duration must yield no slots, got %v", s)
	}
	if s := computeSlots(from, to, from, 30, 0, nil, loc, nil); s != nil {
		t.Errorf("no windows must yield no slots, got %v", s)
	}
}

// TestComputeSlotsRejectsOffWindow locks the availability-enforcement regression:
// handleBook validates a requested start by requiring it to be a member of
// computeSlots, so computeSlots must never emit an off-hours, wrong-day, or past
// time — otherwise the write path would accept it (the NO-GO finding).
func TestComputeSlotsRejectsOffWindow(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 7, 27, 0, 0, 0, 0, loc)
	wd := int(from.Weekday())
	to := from.Add(24 * time.Hour)
	now := from.Add(-48 * time.Hour)
	windows := []availWindow{{Weekday: wd, StartMinute: 9 * 60, EndMinute: 11 * 60}}
	slots := computeSlots(from, to, now, 30, 0, windows, loc, nil)

	offHours := from.Add(3 * time.Hour)                 // 03:00 — outside 9–11
	wrongDay := from.Add(3*24*time.Hour + 10*time.Hour) // a different weekday, 10:00
	past := from.Add(-2 * time.Hour)                    // before the window even opens
	for _, bad := range []time.Time{offHours.UTC(), wrongDay.UTC(), past.UTC()} {
		for _, s := range slots {
			if s.Equal(bad) {
				t.Errorf("computeSlots emitted an off-window/past time %v — handleBook would wrongly accept it", bad)
			}
		}
	}
	if len(slots) != 4 {
		t.Fatalf("sanity: want 4 in-window slots, got %d", len(slots))
	}
}
