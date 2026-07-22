package migrations

import (
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

// Scheduling data model — the native Hanzo Base + IAM backend for booking pages
// (event types, availability, bookings). This is the "invite me to a time"
// side of calendaring, complementing the connected-account calendar SYNC model
// in 1780500000_connect_messaging_calendar.go: a booking can be checked against
// the host's synced calendarEvent rows and written back to their calendar.
//
// Ownership is IAM-native — every row carries the host's IAM user id in `owner`.
// A host has full CRUD over their own event types, schedules and bookings.
// Event types are publicly readable (a booking page is public), while the
// schedule and bookings stay owner-scoped. Bookings are created by the booking
// endpoint in superuser context after it validates availability and conflicts,
// so CreateRule is nil at the collection layer (endpoint-gated writes, the same
// shape the sync plugins use).

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		for _, create := range []func(core.App) error{
			createAvailabilityScheduleCollection,
			createEventTypeCollection,
			createBookingCollection,
			createBookingAttendeeCollection,
		} {
			if err := create(txApp); err != nil {
				return err
			}
		}
		return nil
	}, func(txApp core.App) error {
		for _, name := range []string{
			"bookingAttendee", "booking", "eventType", "availabilitySchedule",
		} {
			if c, err := txApp.FindCollectionByNameOrId(name); err == nil {
				if err := txApp.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// hostOwned builds a collection the IAM host fully owns and privately manages —
// reads and writes are scoped to `owner = @request.auth.id`.
func hostOwned(name string) *core.Collection {
	c := core.NewBaseCollection(name)
	own := "owner = @request.auth.id"
	c.ListRule = types.Pointer(own)
	c.ViewRule = types.Pointer(own)
	c.CreateRule = types.Pointer(own)
	c.UpdateRule = types.Pointer(own)
	c.DeleteRule = types.Pointer(own)
	c.Fields.Add(&core.TextField{Name: "owner", Required: true}) // IAM host user id
	return c
}

// endpointWritten builds a host-readable collection whose rows are created by the
// booking endpoint (superuser), not directly by clients — a public booker never
// gets write access to the raw collection; the endpoint validates first.
func endpointWritten(name string) *core.Collection {
	c := hostOwned(name)
	c.CreateRule = nil // booking endpoint / superuser only
	return c
}

// createAvailabilityScheduleCollection — a host's named weekly availability.
func createAvailabilityScheduleCollection(txApp core.App) error {
	c := hostOwned("availabilitySchedule")
	c.Fields.Add(&core.TextField{Name: "name", Required: true})
	c.Fields.Add(&core.TextField{Name: "timezone", Required: true}) // IANA tz, e.g. America/Los_Angeles
	c.Fields.Add(&core.JSONField{Name: "weekly"})                   // [{weekday:0-6, startMinute, endMinute}]
	c.Fields.Add(&core.JSONField{Name: "overrides"})               // [{date:"YYYY-MM-DD", windows:[...] | unavailable:true}]
	c.Fields.Add(&core.BoolField{Name: "isDefault"})
	addTimestamps(c)
	c.AddIndex("idx_sched_owner_name", true, "owner, name", "")
	return txApp.Save(c)
}

// createEventTypeCollection — a bookable meeting type. Publicly readable so a
// booking page renders without auth; only the host can change it.
func createEventTypeCollection(txApp core.App) error {
	c := hostOwned("eventType")
	// The collection stays owner-scoped. Public booking pages read the event type
	// through the scheduling plugin's handler (a filtered superuser query that
	// returns only active types and omits internal fields), so the raw records
	// API never enumerates other hosts' event types or leaks the location.
	c.Fields.Add(&core.TextField{Name: "title", Required: true})
	c.Fields.Add(&core.TextField{Name: "slug", Required: true})
	c.Fields.Add(&core.TextField{Name: "description"})
	c.Fields.Add(&core.NumberField{Name: "durationMinutes", Required: true})
	c.Fields.Add(&core.SelectField{Name: "locationType", MaxSelect: 1, Values: []string{"video", "phone", "in_person", "custom"}})
	c.Fields.Add(&core.TextField{Name: "location"}) // link template, number, or address
	c.Fields.Add(refField("availabilitySchedule"))  // which schedule governs open slots
	c.Fields.Add(&core.NumberField{Name: "bufferBeforeMinutes"})
	c.Fields.Add(&core.NumberField{Name: "bufferAfterMinutes"})
	c.Fields.Add(&core.NumberField{Name: "minimumNoticeMinutes"})
	c.Fields.Add(&core.TextField{Name: "timezone"}) // host tz for slot rendering
	c.Fields.Add(&core.BoolField{Name: "active"})
	addTimestamps(c)
	c.AddIndex("idx_eventtype_owner_slug", true, "owner, slug", "")
	return txApp.Save(c)
}

// createBookingCollection — a booked appointment. The primary attendee is inline;
// guests live in bookingAttendee. Written by the booking endpoint after it checks
// the schedule and the host's synced calendar for conflicts.
func createBookingCollection(txApp core.App) error {
	c := endpointWritten("booking")
	c.Fields.Add(refField("eventType"))
	c.Fields.Add(&core.DateField{Name: "startsAt"})
	c.Fields.Add(&core.DateField{Name: "endsAt"})
	c.Fields.Add(&core.TextField{Name: "timezone"}) // attendee tz at booking time
	c.Fields.Add(&core.SelectField{Name: "status", MaxSelect: 1, Values: []string{"pending", "accepted", "cancelled", "rescheduled"}})
	c.Fields.Add(&core.TextField{Name: "title"})
	c.Fields.Add(&core.TextField{Name: "description"})
	c.Fields.Add(&core.TextField{Name: "location"})
	c.Fields.Add(&core.TextField{Name: "attendeeName", Required: true, Max: 200})
	c.Fields.Add(&core.EmailField{Name: "attendeeEmail", Required: true})
	c.Fields.Add(&core.TextField{Name: "attendeeTimezone", Max: 80})
	c.Fields.Add(&core.TextField{Name: "attendeeNotes", Max: 4000})
	c.Fields.Add(&core.TextField{Name: "uid", Required: true}) // opaque token for the attendee's manage link
	c.Fields.Add(refField("calendarEvent"))                    // the event written back to the host's calendar
	c.Fields.Add(&core.TextField{Name: "cancelReason", Max: 500})
	c.Fields.Add(&core.DateField{Name: "cancelledAt"})
	addTimestamps(c)
	// UNIQUE partial index: a host cannot hold two live bookings at the same start
	// — the backstop against a check-then-write double-book race. Cancelled rows
	// are excluded so a freed slot can be re-booked.
	c.AddIndex("idx_booking_owner_start", true, "owner, startsAt", "status != 'cancelled'")
	c.AddIndex("idx_booking_uid", true, "uid", "")
	return txApp.Save(c)
}

// createBookingAttendeeCollection — additional guests on a booking.
func createBookingAttendeeCollection(txApp core.App) error {
	c := endpointWritten("bookingAttendee")
	c.Fields.Add(refField("booking"))
	c.Fields.Add(&core.TextField{Name: "name"})
	c.Fields.Add(&core.TextField{Name: "email", Required: true})
	c.Fields.Add(&core.TextField{Name: "timezone"})
	c.Fields.Add(&core.SelectField{Name: "responseStatus", MaxSelect: 1, Values: []string{"needs_action", "declined", "tentative", "accepted"}})
	addTimestamps(c)
	c.AddIndex("idx_bookingattendee_booking", false, "booking", "")
	return txApp.Save(c)
}
