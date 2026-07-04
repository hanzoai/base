package migrations_test

import (
	"testing"

	"github.com/hanzoai/base/tests"
)

// Proves the connect/messaging/calendar migration applies at boot and that all
// ten collections exist with the intended access model: owner-scoped reads and
// plugin-only writes (the sync plugin is the sole writer). Guards against a
// regression that would leave the CRM sync model unwritable-by-plugin or
// world-readable.
func TestConnectMessagingCalendar_BootApplies(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	want := []string{
		"connectedAccount",
		"messageChannel", "messageThread", "message", "messageParticipant", "messageAssociation",
		"calendarChannel", "calendarEvent", "calendarEventParticipant", "calendarEventAssociation",
	}
	for _, name := range want {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatalf("collection %q not created by migration: %v", name, err)
		}
		if c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
			t.Errorf("%q must be plugin-only-write (nil create/update/delete rules), got C=%v U=%v D=%v",
				name, c.CreateRule, c.UpdateRule, c.DeleteRule)
		}
		if c.ListRule == nil || *c.ListRule != "owner = @request.auth.id" {
			t.Errorf("%q must be owner-scoped readable, got ListRule=%v", name, c.ListRule)
		}
	}
}
