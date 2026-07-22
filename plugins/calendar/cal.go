package calendar

import (
	"hash/fnv"
	"net/http"
	"time"

	"github.com/hanzoai/base/core"
)

// cal.go is the adapter that maps Base scheduling records to Cal.com's API-v2 JSON
// shapes and wraps them in Cal's response envelope. It is the single place the wire
// contract lives, so the privacy decisions (what the public booker may and may not
// see) are all here, next to the shapes they guard.

// --- response envelope ---
//
// Cal's ApiResponse is {status:"success", data} on success, {status:"success"} for
// a data-less success, and {status:"error", error:{message}} on failure. The atom's
// http client keys entirely off status === "success".

func calOK(e *core.RequestEvent, data any) error {
	return e.JSON(http.StatusOK, map[string]any{"status": "success", "data": data})
}

func calOKStatus(e *core.RequestEvent, code int, data any) error {
	return e.JSON(code, map[string]any{"status": "success", "data": data})
}

func calOKEmpty(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"status": "success"})
}

func calError(e *core.RequestEvent, code int, msg string) error {
	return e.JSON(code, map[string]any{"status": "error", "error": map[string]any{"message": msg}})
}

// stableNumericID derives a deterministic positive integer from a Base record id.
// Cal types eventTypeId/bookingId as numbers; Base ids are opaque strings. The
// number is display/hold-key only — every data lookup is by (owner, slug) or by the
// booking uid, never by this id — so a hash collision is harmless.
func stableNumericID(s string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	v := int64(h.Sum32() & 0x7fffffff)
	if v == 0 {
		v = 1
	}
	return v
}

// --- public event (useAtomGetPublicEvent) ---

// publicEventDTO renders the event for the unauthenticated booker. It carries what
// the Booker needs to render and NOTHING that leaks the host: no email, no internal
// ids, and no raw meeting location — only the booking fields, layouts, and a public
// profile whose handle is the owner's public booking username.
func publicEventDTO(owner string, et, sched *core.Record) map[string]any {
	layouts := bookerLayoutsDTO()
	name := profileName(owner)

	var schedule any
	if sched != nil {
		schedule = map[string]any{"id": stableNumericID(sched.Id), "timeZone": sched.GetString("timezone")}
	}

	return map[string]any{
		"id":                              stableNumericID(et.Id),
		"title":                           et.GetString("title"),
		"slug":                            et.GetString("slug"),
		"length":                          et.GetInt("durationMinutes"),
		"description":                     et.GetString("description"),
		"hidden":                          false,
		"isDynamic":                       false,
		"requiresConfirmation":            false,
		"requiresBookerEmailVerification": false,
		"price":                           0,
		"currency":                        "usd",
		"lockTimeZoneToggleOnBookingPage": false,
		"seatsPerTimeSlot":                nil,
		"seatsShowAttendees":              false,
		"seatsShowAvailabilityCount":      false,
		"schedulingType":                  nil,
		"recurringEvent":                  nil,
		"metadata":                        map[string]any{},
		"customInputs":                    []any{},
		// Locations carry only their type, never the secret link/number/address —
		// which is revealed post-booking via the booking uid. Phase 1 keeps this
		// empty (single host location applies) so nothing pre-booking can leak.
		"locations":     locationsDTO(et),
		"bookingFields": defaultBookingFields(),
		"bookerLayouts": layouts,
		"schedule":      schedule,
		"timeZone":      nil, // attendee picks their timezone; event is not tz-locked
		"users":         []any{userDTO(owner, name)},
		"profile": map[string]any{
			"name":           name,
			"username":       owner,
			"image":          "",
			"weekStart":      "Sunday",
			"brandColor":     "#292929",
			"darkBrandColor": "#fafafa",
			"theme":          nil,
			"bookerLayouts":  layouts,
		},
		"entity": map[string]any{
			"considerUnpublished":      false,
			"fromRedirectOfNonOrgLink": true,
			"orgSlug":                  nil,
			"teamSlug":                 nil,
			"name":                     nil,
			"hideProfileLink":          false,
		},
	}
}

// profileName is the host's public display name. We do not store a separate display
// name, so the public booking handle (owner) is used — it is already public in the
// URL and carries no PII beyond the handle itself.
func profileName(owner string) string { return owner }

func userDTO(owner, name string) map[string]any {
	return map[string]any{
		"name":           name,
		"username":       owner,
		"weekStart":      "Sunday",
		"organizationId": nil,
		"avatarUrl":      nil,
		"bookerUrl":      "",
		"profile": map[string]any{
			"username":     owner,
			"name":         name,
			"organization": nil,
		},
	}
}

// locationsDTO returns the privacy-filtered locations for the public event. Phase 1
// returns none (the single host location is applied at booking time and revealed to
// the confirmed attendee), so no meeting link/number/address can leak pre-booking.
func locationsDTO(_ *core.Record) []any { return []any{} }

func bookerLayoutsDTO() map[string]any {
	return map[string]any{
		"enabledLayouts": []string{"month_view", "week_view", "column_view"},
		"defaultLayout":  "month_view",
	}
}

// defaultBookingFields reproduces Cal's system booking fields so the Booker renders
// its standard Name / Email / Notes form. Phone, location, guests and title are
// hidden in Phase 1 (single-location, no guest fan-out); rescheduleReason shows only
// in the reschedule view. Structure mirrors getBookingFields.ts.
func defaultBookingFields() []map[string]any {
	src := []map[string]any{{"label": "Default", "id": "default", "type": "default"}}
	return []map[string]any{
		{"type": "name", "name": "name", "editable": "system", "defaultLabel": "your_name", "required": true, "sources": src},
		{"type": "email", "name": "email", "editable": "system-but-optional", "defaultLabel": "email_address", "required": true, "sources": src},
		{"type": "phone", "name": "attendeePhoneNumber", "editable": "system-but-optional", "defaultLabel": "phone_number", "required": false, "hidden": true, "sources": src},
		{"type": "text", "name": "title", "editable": "system-but-optional", "defaultLabel": "what_is_this_meeting_about", "required": false, "hidden": true, "sources": src},
		{"type": "textarea", "name": "notes", "editable": "system-but-optional", "defaultLabel": "additional_notes", "required": false, "sources": src},
		{"type": "multiemail", "name": "guests", "editable": "system-but-optional", "defaultLabel": "additional_guests", "required": false, "hidden": true, "sources": src},
		{"type": "textarea", "name": "rescheduleReason", "editable": "system-but-optional", "defaultLabel": "reason_for_reschedule", "required": false,
			"views": []map[string]any{{"id": "reschedule", "label": "Reschedule View"}}, "sources": src},
	}
}

// --- slots (useAvailableSlots) ---

// slotsDTO groups open start times by their local date in the requested timezone,
// as Cal's Booker expects: {slots: {"YYYY-MM-DD": [{time}]}}. Advisory holds hide a
// slot from the listing without affecting the write gate.
func slotsDTO(slots, held []time.Time, tz string) map[string]any {
	heldSet := make(map[int64]struct{}, len(held))
	for _, h := range held {
		heldSet[h.Unix()] = struct{}{}
	}
	loc := loadLoc(tz)
	out := map[string]any{}
	for _, s := range slots {
		if _, ok := heldSet[s.Unix()]; ok {
			continue
		}
		day := s.In(loc).Format("2006-01-02")
		list, _ := out[day].([]map[string]any)
		out[day] = append(list, map[string]any{"time": s.UTC().Format(time.RFC3339)})
	}
	return map[string]any{"slots": out}
}

func emptySlotsDTO() map[string]any {
	return map[string]any{"slots": map[string]any{}}
}

// --- bookings (useCreateBooking / confirmation / reschedule) ---

// bookingDTO renders a booking for the create response, the confirmation view and
// the reschedule prefill. It is addressed by the booking's opaque uid, so it may
// reveal the real meeting location (the confirmed attendee needs it) and the
// attendee's own contact details — never the host's email.
func bookingDTO(rec *core.Record) map[string]any {
	start := rec.GetDateTime("startsAt").Time().UTC()
	end := rec.GetDateTime("endsAt").Time().UTC()
	return map[string]any{
		"uid":         rec.GetString("uid"),
		"id":          stableNumericID(rec.GetString("uid")),
		"title":       rec.GetString("title"),
		"description": rec.GetString("description"),
		"status":      rec.GetString("status"),
		"start":       start.Format(time.RFC3339),
		"end":         end.Format(time.RFC3339),
		"startTime":   start.Format(time.RFC3339),
		"endTime":     end.Format(time.RFC3339),
		"eventTypeId": stableNumericID(rec.GetString("eventType")),
		"location":    rec.GetString("location"),
		"metadata":    map[string]any{},
		"attendees": []any{map[string]any{
			"name":     rec.GetString("attendeeName"),
			"email":    rec.GetString("attendeeEmail"),
			"timeZone": rec.GetString("attendeeTimezone"),
		}},
		"user": map[string]any{
			"username": rec.GetString("owner"),
			"name":     profileName(rec.GetString("owner")),
		},
		"responses": map[string]any{
			"name":  rec.GetString("attendeeName"),
			"email": rec.GetString("attendeeEmail"),
			"notes": rec.GetString("attendeeNotes"),
		},
	}
}

// --- me (useMe) ---

// meDTO is the anonymous /me shape. Public booking sends no access token so the atom
// never calls this; if called it returns only neutral rendering preferences — no id,
// name or email — so it can never leak a real user.
func meDTO() map[string]any {
	return map[string]any{
		"username":       "",
		"organizationId": 0,
		"timeFormat":     12,
		"weekStart":      "Sunday",
		"timeZone":       "UTC",
	}
}
