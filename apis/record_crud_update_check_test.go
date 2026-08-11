package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// userToken is a signed auth token for users/4q1xlclmfloku33 in the test data.
const userToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo"

// TestUpdateRuleChecksTheRowItWrites pins what Postgres calls WITH CHECK.
//
// `owner = @request.auth.id` on update means two things there, not one: which
// rows you may touch, and what the row is allowed to look like afterwards. When
// a policy omits the second, Postgres reuses the first — so a user who may edit
// their own row still cannot edit it into somebody else's.
//
// Base applies the rule only to the row as it EXISTS. The write is then submitted
// unchecked, so the same rule permits handing the row away, and with it any
// column the rule was protecting. Ported verbatim from Supabase, a policy that
// reads as ownership is enforced as a one-time entry ticket.
func TestUpdateRuleChecksTheRowItWrites(t *testing.T) {
	t.Parallel()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		c := core.NewBaseCollection("ownedrows")
		c.Fields.Add(&core.TextField{Name: "owner"})
		c.Fields.Add(&core.TextField{Name: "note"})
		rule := "owner = @request.auth.id"
		c.ListRule = &rule
		c.ViewRule = &rule
		c.UpdateRule = &rule
		if err := app.Save(c); err != nil {
			t.Fatal(err)
		}

		mine := core.NewRecord(c)
		mine.Id = "ownedrow0000001"
		mine.Set("owner", "4q1xlclmfloku33")
		mine.Set("note", "before")
		if err := app.Save(mine); err != nil {
			t.Fatal(err)
		}
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "editing a field I own is still allowed",
			Method:         http.MethodPatch,
			URL:            "/v1/collections/ownedrows/records/ownedrow0000001",
			Body:           strings.NewReader(`{"note":"after"}`),
			Headers:        map[string]string{"Authorization": userToken},
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"note":"after"`,
				`"owner":"4q1xlclmfloku33"`,
			},
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordUpdateRequest":      1,
				"OnRecordEnrich":             1,
				"OnModelUpdate":              1,
				"OnModelUpdateExecute":       1,
				"OnModelAfterUpdateSuccess":  1,
				"OnRecordUpdate":             1,
				"OnRecordUpdateExecute":      1,
				"OnRecordAfterUpdateSuccess": 1,
				"OnModelValidate":            1,
				"OnRecordValidate":           1,
			},
		},
		{
			Name:           "handing the row to somebody else is refused",
			Method:         http.MethodPatch,
			URL:            "/v1/collections/ownedrows/records/ownedrow0000001",
			Body:           strings.NewReader(`{"owner":"someoneelse00x"}`),
			Headers:        map[string]string{"Authorization": userToken},
			BeforeTestFunc: setup,
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"status":400`,
			},
			// Refused before the request hook fires, which is where the create
			// rule is checked too — a write that the rule will not allow never
			// reaches the handlers that would act on it.
			ExpectedEvents: map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
