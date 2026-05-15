package apis_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/types"
)

func TestEnrichRecords(t *testing.T) {
	t.Parallel()

	// mock test data
	// ---
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	freshRecords := func(records []*core.Record) []*core.Record {
		result := make([]*core.Record, len(records))
		for i, r := range records {
			result[i] = r.Fresh()
		}
		return result
	}

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatal(err)
	}

	usersRecords, err := app.FindRecordsByIds("users", []string{"4q1xlclmfloku33", "bgs820n361vj1qd"})
	if err != nil {
		t.Fatal(err)
	}

	nologinRecords, err := app.FindRecordsByIds("nologin", []string{"dc49k6jgejn40h3", "oos036e9xvqeexy"})
	if err != nil {
		t.Fatal(err)
	}

	demo1Records, err := app.FindRecordsByIds("demo1", []string{"al1h9ijdeojtsjy", "84nmscqy84lsi1t"})
	if err != nil {
		t.Fatal(err)
	}

	demo5Records, err := app.FindRecordsByIds("demo5", []string{"la4y2w4o98acwuj", "qjeql998mtp1azp"})
	if err != nil {
		t.Fatal(err)
	}

	// temp update the view rule to ensure that request context is set to "expand"
	demo4, err := app.FindCollectionByNameOrId("demo4")
	if err != nil {
		t.Fatal(err)
	}
	demo4.ViewRule = types.Pointer("@request.context = 'expand'")
	if err := app.Save(demo4); err != nil {
		t.Fatal(err)
	}
	// ---

	scenarios := []struct {
		name           string
		auth           *core.Record
		records        []*core.Record
		queryExpand    string
		defaultExpands []string
		expected       []string
		notExpected    []string
	}{
		// email visibility checks
		{
			name:           "[emailVisibility] guest",
			auth:           nil,
			records:        freshRecords(usersRecords),
			queryExpand:    "",
			defaultExpands: nil,
			expected: []string{
				`"customField":"123"`,
				`"test3@example.com"`, // emailVisibility=true
			},
			notExpected: []string{
				`"test@example.com"`,
			},
		},
		{
			name:           "[emailVisibility] owner",
			auth:           user,
			records:        freshRecords(usersRecords),
			queryExpand:    "",
			defaultExpands: nil,
			expected: []string{
				`"customField":"123"`,
				`"test3@example.com"`, // emailVisibility=true
				`"test@example.com"`,  // owner
			},
		},
		{
			name:           "[emailVisibility] manager",
			auth:           user,
			records:        freshRecords(nologinRecords),
			queryExpand:    "",
			defaultExpands: nil,
			expected: []string{
				`"customField":"123"`,
				`"test3@example.com"`,
				`"test@example.com"`,
			},
		},
		{
			name:           "[emailVisibility] superuser",
			auth:           superuser,
			records:        freshRecords(nologinRecords),
			queryExpand:    "",
			defaultExpands: nil,
			expected: []string{
				`"customField":"123"`,
				`"test3@example.com"`,
				`"test@example.com"`,
			},
		},
		{
			name:           "[emailVisibility + expand] recursive auth rule checks (regular user)",
			auth:           user,
			records:        freshRecords(demo1Records),
			queryExpand:    "",
			defaultExpands: []string{"rel_many"},
			expected: []string{
				`"customField":"123"`,
				`"expand":{"rel_many"`,
				`"expand":{}`,
				`"test@example.com"`,
			},
			notExpected: []string{
				`"id":"bgs820n361vj1qd"`,
				`"id":"oap640cot4yru2s"`,
			},
		},
		{
			name:           "[emailVisibility + expand] recursive auth rule checks (superuser)",
			auth:           superuser,
			records:        freshRecords(demo1Records),
			queryExpand:    "",
			defaultExpands: []string{"rel_many"},
			expected: []string{
				`"customField":"123"`,
				`"test@example.com"`,
				`"expand":{"rel_many"`,
				`"id":"bgs820n361vj1qd"`,
				`"id":"4q1xlclmfloku33"`,
				`"id":"oap640cot4yru2s"`,
			},
			notExpected: []string{
				`"expand":{}`,
			},
		},

		// expand checks
		{
			name:           "[expand] guest (query)",
			auth:           nil,
			records:        freshRecords(usersRecords),
			queryExpand:    "rel",
			defaultExpands: nil,
			expected: []string{
				`"customField":"123"`,
				`"expand":{"rel"`,
				`"id":"llvuca81nly1qls"`,
				`"id":"0yxhwia2amd8gec"`,
			},
			notExpected: []string{
				`"expand":{}`,
			},
		},
		{
			name:           "[expand] guest (default expands)",
			auth:           nil,
			records:        freshRecords(usersRecords),
			queryExpand:    "",
			defaultExpands: []string{"rel"},
			expected: []string{
				`"customField":"123"`,
				`"expand":{"rel"`,
				`"id":"llvuca81nly1qls"`,
				`"id":"0yxhwia2amd8gec"`,
			},
		},
		{
			name:           "[expand] @request.context=expand check",
			auth:           nil,
			records:        freshRecords(demo5Records),
			queryExpand:    "rel_one",
			defaultExpands: []string{"rel_many"},
			expected: []string{
				`"customField":"123"`,
				`"expand":{}`,
				`"expand":{"`,
				`"rel_many":[{`,
				`"rel_one":{`,
				`"id":"i9naidtvr6qsgb4"`,
				`"id":"qzaqccwrmva4o1n"`,
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			app, _ := tests.NewTestApp()
			defer app.Cleanup()

			app.OnRecordEnrich().BindFunc(func(e *core.RecordEnrichEvent) error {
				e.Record.WithCustomData(true)
				e.Record.Set("customField", "123")
				return e.Next()
			})

			req := httptest.NewRequest(http.MethodGet, "/?expand="+s.queryExpand, nil)
			rec := httptest.NewRecorder()

			requestEvent := new(core.RequestEvent)
			requestEvent.App = app
			requestEvent.Request = req
			requestEvent.Response = rec
			requestEvent.Auth = s.auth

			err := apis.EnrichRecords(requestEvent, s.records, s.defaultExpands...)
			if err != nil {
				t.Fatal(err)
			}

			raw, err := json.Marshal(s.records)
			if err != nil {
				t.Fatal(err)
			}
			rawStr := string(raw)

			for _, str := range s.expected {
				if !strings.Contains(rawStr, str) {
					t.Fatalf("Expected\n%q\nin\n%v", str, rawStr)
				}
			}

			for _, str := range s.notExpected {
				if strings.Contains(rawStr, str) {
					t.Fatalf("Didn't expected\n%q\nin\n%v", str, rawStr)
				}
			}
		})
	}
}

func TestRecordAuthResponseAuthRuleCheck(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	event := new(core.RequestEvent)
	event.App = app
	event.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	event.Response = httptest.NewRecorder()

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}

	scenarios := []struct {
		name        string
		rule        *string
		expectError bool
	}{
		{
			"admin only rule",
			nil,
			true,
		},
		{
			"empty rule",
			types.Pointer(""),
			false,
		},
		{
			"false rule",
			types.Pointer("1=2"),
			true,
		},
		{
			"true rule",
			types.Pointer("1=1"),
			false,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			user.Collection().AuthRule = s.rule

			err := apis.RecordAuthResponse(event, user, "", nil)

			hasErr := err != nil
			if s.expectError != hasErr {
				t.Fatalf("Expected hasErr %v, got %v (%v)", s.expectError, hasErr, err)
			}

			// in all cases login alert shouldn't be send because of the empty auth method
			if app.TestMailer.TotalSend() != 0 {
				t.Fatalf("Expected no emails send, got %d:\n%v", app.TestMailer.TotalSend(), app.TestMailer.LastMessage().HTML)
			}

			if !hasErr {
				return
			}

			apiErr, ok := err.(*router.ApiError)

			if !ok || apiErr == nil {
				t.Fatalf("Expected ApiError, got %v", apiErr)
			}

			if apiErr.Status != http.StatusForbidden {
				t.Fatalf("Expected ApiError.Status %d, got %d", http.StatusForbidden, apiErr.Status)
			}
		})
	}
}
