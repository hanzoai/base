package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func TestRecordConfirmEmailChange(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "not an auth collection",
			Method:          http.MethodPost,
			URL:             "/api/collections/demo1/confirm-email-change",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:           "empty data",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/confirm-email-change",
			Body:           strings.NewReader(``),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":`,
				`"token":{"code":"validation_required"`,
				`"password":{"code":"validation_required"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:            "invalid data",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/confirm-email-change",
			Body:            strings.NewReader(`{"token`),
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired token and correct password",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njg4MTI4NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.9BHGy4lBsyZKoe84xh6eloSh-JBCGQkXaV1edtbKNQU",
				"password":"1234567890"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"token":{`,
				`"code":"validation_invalid_token"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "non-email change token",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
				"password":"1234567890"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"token":{`,
				`"code":"validation_invalid_token_payload"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "valid token and incorrect password",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjQ5MjI0MTY0NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.0CPwQ5HzvruHJeYZVSEv69Fx1RbdxHi4aNFqX81lAWs",
				"password":"1234567891"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"password":{`,
				`"code":"validation_invalid_password"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "valid token and correct password",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjQ5MjI0MTY0NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.0CPwQ5HzvruHJeYZVSEv69Fx1RbdxHi4aNFqX81lAWs",
				"password":"1234567890"
			}`),
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{
				"*":                                 0,
				"OnRecordConfirmEmailChangeRequest": 1,
				"OnModelUpdate":                     1,
				"OnModelUpdateExecute":              1,
				"OnModelAfterUpdateSuccess":         1,
				"OnModelValidate":                   1,
				"OnRecordUpdate":                    1,
				"OnRecordUpdateExecute":             1,
				"OnRecordAfterUpdateSuccess":        1,
				"OnRecordValidate":                  1,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				_, err := app.FindAuthRecordByEmail("users", "change@example.com")
				if err != nil {
					t.Fatalf("Expected to find user with email %q, got error: %v", "change@example.com", err)
				}
			},
		},
		{
			Name:   "valid token in different auth collection",
			Method: http.MethodPost,
			URL:    "/api/collections/clients/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjQ5MjI0MTY0NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.0CPwQ5HzvruHJeYZVSEv69Fx1RbdxHi4aNFqX81lAWs",
				"password":"1234567890"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"token":{"code":"validation_token_collection_mismatch"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "OnRecordConfirmEmailChangeRequest tx body write check",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjQ5MjI0MTY0NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.0CPwQ5HzvruHJeYZVSEv69Fx1RbdxHi4aNFqX81lAWs",
				"password":"1234567890"
			}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnRecordConfirmEmailChangeRequest().BindFunc(func(e *core.RecordConfirmEmailChangeRequestEvent) error {
					original := e.App
					return e.App.RunInTransaction(func(txApp core.App) error {
						e.App = txApp
						defer func() { e.App = original }()

						if err := e.Next(); err != nil {
							return err
						}

						return e.BadRequestError("TX_ERROR", nil)
					})
				})
			},
			ExpectedStatus:  400,
			ExpectedEvents:  map[string]int{"OnRecordConfirmEmailChangeRequest": 1},
			ExpectedContent: []string{"TX_ERROR"},
		},

		// rate limit checks
		// -----------------------------------------------------------
		{
			Name:   "RateLimit rule - users:confirmEmailChange",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjQ5MjI0MTY0NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.0CPwQ5HzvruHJeYZVSEv69Fx1RbdxHi4aNFqX81lAWs",
				"password":"1234567890"
			}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 100, Label: "*:confirmEmailChange"},
					{MaxRequests: 0, Label: "users:confirmEmailChange"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "RateLimit rule - *:confirmEmailChange",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-email-change",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjQ5MjI0MTY0NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwibmV3RW1haWwiOiJjaGFuZ2VAZXhhbXBsZS5jb20iLCJ0eXBlIjoiZW1haWxDaGFuZ2UifQ.0CPwQ5HzvruHJeYZVSEv69Fx1RbdxHi4aNFqX81lAWs",
				"password":"1234567890"
			}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 0, Label: "*:confirmEmailChange"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
