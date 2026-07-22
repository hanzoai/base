package calendar

import (
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/security"
)

// redSetup boots a real Base app with the scheduling migration applied and
// seeds one host + default schedule (9:00-11:00 on the slot day, 30-min events,
// 15-min buffers both sides) + one active event type. Returns the plugin, the
// event-type record, the owner id, and a valid future grid start (09:00 UTC).
func redSetup(t *testing.T) (*plugin, *core.Record, string, time.Time) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	owner := "host_" + security.RandomString(6)

	// A grid start well in the future so minimum-notice never filters it.
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 7)
	start := day.Add(9 * time.Hour) // 09:00 UTC on that day
	wd := int(start.Weekday())

	schedCol, err := app.FindCollectionByNameOrId("availabilitySchedule")
	if err != nil {
		t.Fatal(err)
	}
	sched := core.NewRecord(schedCol)
	sched.Set("owner", owner)
	sched.Set("name", "default")
	sched.Set("timezone", "UTC")
	sched.Set("weekly", []map[string]any{{"weekday": wd, "startMinute": 9 * 60, "endMinute": 11 * 60}})
	sched.Set("isDefault", true)
	if err := app.Save(sched); err != nil {
		t.Fatalf("save schedule: %v", err)
	}

	etCol, err := app.FindCollectionByNameOrId("eventType")
	if err != nil {
		t.Fatal(err)
	}
	et := core.NewRecord(etCol)
	et.Set("owner", owner)
	et.Set("title", "Intro")
	et.Set("slug", "intro")
	et.Set("durationMinutes", 30)
	et.Set("bufferBeforeMinutes", 15)
	et.Set("bufferAfterMinutes", 15)
	et.Set("minimumNoticeMinutes", 0)
	et.Set("availabilitySchedule", sched.Id)
	et.Set("active", true)
	if err := app.Save(et); err != nil {
		t.Fatalf("save eventType: %v", err)
	}

	return &plugin{app: app}, et, owner, start
}

func (p *plugin) newBooking(owner string, et *core.Record, start time.Time, status string) *core.Record {
	col, _ := p.app.FindCollectionByNameOrId("booking")
	rec := core.NewRecord(col)
	rec.Set("owner", owner)
	rec.Set("eventType", et.Id)
	rec.Set("startsAt", start)
	rec.Set("endsAt", start.Add(time.Duration(et.GetInt("durationMinutes"))*time.Minute))
	rec.Set("status", status)
	rec.Set("title", et.GetString("title"))
	rec.Set("attendeeName", "A")
	rec.Set("attendeeEmail", "a@example.com")
	rec.Set("uid", security.RandomString(24))
	return rec
}

// bookTx mirrors handleBook's authoritative write path exactly: the final
// isOpenSlot re-check and Save inside one RunInTransaction.
func (p *plugin) bookTx(owner string, et *core.Record, start time.Time) error {
	rec := p.newBooking(owner, et, start, "accepted")
	return p.app.RunInTransaction(func(txApp core.App) error {
		if !p.isOpenSlot(txApp, owner, et, start) {
			return errSlotTaken
		}
		return txApp.Save(rec)
	})
}

func liveBookings(t *testing.T, p *plugin, owner string) int {
	t.Helper()
	recs, err := p.app.FindAllRecords("booking")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range recs {
		if r.GetString("owner") == owner && r.GetString("status") != "cancelled" {
			n++
		}
	}
	return n
}

// TestRed_ConcurrentOverlappingBookings fires two concurrent bookings at two
// DIFFERENT grid starts (09:00 and 09:30) that overlap once the 15-min buffers
// are applied. The partial unique index on (owner,startsAt) does NOT catch this
// (different startsAt); only the in-transaction isOpenSlot re-check can. This
// proves whether single-process write serialization actually closes the overlap
// TOCTOU on the default (SQLite) backend.
func TestRed_ConcurrentOverlappingBookings(t *testing.T) {
	p, et, owner, start := redSetup(t)
	nine := start
	nineThirty := start.Add(30 * time.Minute)

	// Sanity: with nothing booked, both are open slots.
	if !p.isOpenSlot(p.app, owner, et, nine) {
		t.Fatal("09:00 should be open initially")
	}
	if !p.isOpenSlot(p.app, owner, et, nineThirty) {
		t.Fatal("09:30 should be open initially")
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	starts := []time.Time{nine, nineThirty}
	wg.Add(2)
	for i := range starts {
		go func(i int) {
			defer wg.Done()
			errs[i] = p.bookTx(owner, et, starts[i])
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		}
	}
	live := liveBookings(t, p, owner)
	t.Logf("concurrent overlapping: successes=%d liveRows=%d errs=%v", ok, live, errs)
	if live != 1 {
		t.Fatalf("DOUBLE-BOOK: expected exactly 1 live booking after concurrent overlap, got %d (successes=%d)", live, ok)
	}
}

// TestRed_PartialUniqueIndex proves the migration's partial UNIQUE index is
// honored by SQLite: two live rows at the same (owner,startsAt) cannot coexist,
// yet a slot freed by cancellation can be re-booked (the WHERE status!='cancelled'
// is respected, not silently dropped into a full unique index).
func TestRed_PartialUniqueIndex(t *testing.T) {
	p, et, owner, start := redSetup(t)

	a := p.newBooking(owner, et, start, "accepted")
	if err := p.app.Save(a); err != nil {
		t.Fatalf("first booking must save: %v", err)
	}
	b := p.newBooking(owner, et, start, "accepted")
	if err := p.app.Save(b); err == nil {
		t.Fatal("SECOND identical-start live booking saved — partial UNIQUE index NOT enforced")
	} else {
		t.Logf("identical-start second booking correctly rejected: %v", err)
	}

	// Cancel the first, then a fresh booking at the same start must succeed.
	a.Set("status", "cancelled")
	if err := p.app.Save(a); err != nil {
		t.Fatalf("cancel must save: %v", err)
	}
	c := p.newBooking(owner, et, start, "accepted")
	if err := p.app.Save(c); err != nil {
		t.Fatalf("re-book after cancel must succeed (partial WHERE must exclude cancelled rows): %v", err)
	}
}

// TestRed_OffWindowRejectedByIsOpenSlot confirms the write gate rejects an
// arbitrary off-grid / off-hours / past start through the very same function
// handleBook calls (not just the pure computeSlots unit).
func TestRed_OffWindowRejectedByIsOpenSlot(t *testing.T) {
	p, et, owner, start := redSetup(t)
	bad := []time.Time{
		start.Add(3 * time.Minute),       // off-grid (not a multiple of 30 from 09:00)
		start.Add(-3 * time.Hour),        // 06:00, before the window opens
		start.Add(6 * time.Hour),         // 15:00, after the window closes
		start.AddDate(0, 0, 1),           // next day, wrong weekday
		time.Now().UTC().Add(-time.Hour), // in the past
	}
	for _, b := range bad {
		if p.isOpenSlot(p.app, owner, et, b) {
			t.Errorf("isOpenSlot accepted a non-slot %v — handleBook would Save it", b)
		}
	}
	if !p.isOpenSlot(p.app, owner, et, start) {
		t.Fatal("sanity: the real 09:00 grid start must be open")
	}
}
