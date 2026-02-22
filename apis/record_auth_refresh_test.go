package apis_test

import (
	"net/http"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func TestRecordAuthRefresh(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "unauthorized",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-refresh",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "superuser trying to refresh the auth of another auth collection",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "auth record + not an auth collection",
			Method: http.MethodPost,
			URL:    "/api/collections/demo1/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "auth record + different auth collection",
			Method: http.MethodPost,
			URL:    "/api/collections/clients/auth-refresh?expand=rel,missing",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "auth record + same auth collection as the token",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-refresh?expand=rel,missing",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":`,
				`"record":`,
				`"id":"4q1xlclmfloku33"`,
				`"emailVisibility":false`,
				`"email":"test@example.com"`, // the owner can always view their email address
				`"expand":`,
				`"rel":`,
				`"id":"llvuca81nly1qls"`,
			},
			NotExpectedContent: []string{
				`"missing":`,
				// should return a different token
				"eyJhbGciOiJIUzI1NiJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoiX3BiX3VzZXJzX2F1dGhfIiwiZXhwIjoyNTI0NjA0NDYxLCJyZWZyZXNoYWJsZSI6dHJ1ZX0.ZT3F0Z3iM-xbGgSG3LEKiEzHrPHr8t8IuHLZGGNuxLo",
			},
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordAuthRefreshRequest": 1,
				"OnRecordAuthRequest":        1,
				"OnRecordEnrich":             2,
			},
		},
		{
			Name:   "auth record + same auth collection as the token but static/unrefreshable",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOmZhbHNlLCJ0eXBlIjoiYXV0aCJ9.tLivKFyLC-1NGPNwBIeYSKMyZN9H4PGqVggbEWeZrvo",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				// should return the same token
				`"token":"eyJhbGciOiJIUzI1NiJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoiX3BiX3VzZXJzX2F1dGhfIiwiZXhwIjoyNTI0NjA0NDYxLCJyZWZyZXNoYWJsZSI6ZmFsc2V9.4IsO6YMsR19crhwl_YWzvRH8pfq2Ri4Gv2dzGyneLak"`,
				`"record":`,
				`"id":"4q1xlclmfloku33"`,
				`"emailVisibility":false`,
				`"email":"test@example.com"`, // the owner can always view their email address
			},
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordAuthRefreshRequest": 1,
				"OnRecordAuthRequest":        1,
				"OnRecordEnrich":             1,
			},
		},
		{
			Name:   "unverified auth record in onlyVerified collection",
			Method: http.MethodPost,
			URL:    "/api/collections/clients/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJ2ODUxcTRyNzkwcmhrbmwiLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoibzF5MGRkMHNwZDc4Nm1kIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.Vk_K1eyZL_I1VD6fWPHfkA_lBmtbw-fwPq3FSfsyoY8",
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordAuthRefreshRequest": 1,
			},
		},
		{
			Name:   "verified auth record in onlyVerified collection",
			Method: http.MethodPost,
			URL:    "/api/collections/clients/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJ2ODUxcTRyNzkwcmhrbmwiLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiZ2szOTBxZWdzNHk0N3duIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.xCGkWuACPNAEUBLQVK4KKp72HzA2aOtWZnP47iBs5os",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":`,
				`"record":`,
				`"id":"gk390qegs4y47wn"`,
				`"verified":true`,
				`"email":"test@example.com"`,
			},
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordAuthRefreshRequest": 1,
				"OnRecordAuthRequest":        1,
				"OnRecordEnrich":             1,
			},
		},
		{
			Name:   "OnRecordAuthRefreshRequest tx body write check",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnRecordAuthRefreshRequest().BindFunc(func(e *core.RecordAuthRefreshRequestEvent) error {
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
			ExpectedEvents:  map[string]int{"OnRecordAuthRefreshRequest": 1},
			ExpectedContent: []string{"TX_ERROR"},
		},

		// rate limit checks
		// -----------------------------------------------------------
		{
			Name:   "RateLimit rule - users:authRefresh",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 100, Label: "*:authRefresh"},
					{MaxRequests: 0, Label: "users:authRefresh"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "RateLimit rule - *:authRefresh",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-refresh",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 0, Label: "*:authRefresh"},
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
