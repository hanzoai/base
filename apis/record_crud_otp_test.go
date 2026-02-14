package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func TestRecordCrudOTPList(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodGet,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"page":1`,
				`"perPage":30`,
				`"totalItems":0`,
				`"totalPages":0`,
				`"items":[]`,
			},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
			},
		},
		{
			Name:   "regular auth with otps",
			Method: http.MethodGet,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records",
			Headers: map[string]string{
				// users, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"page":1`,
				`"perPage":30`,
				`"totalItems":1`,
				`"totalPages":1`,
				`"id":"user1_0"`,
			},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
				"OnRecordEnrich":       1,
			},
		},
		{
			Name:   "regular auth without otps",
			Method: http.MethodGet,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records",
			Headers: map[string]string{
				// clients, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJ2ODUxcTRyNzkwcmhrbmwiLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiZ2szOTBxZWdzNHk0N3duIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.xCGkWuACPNAEUBLQVK4KKp72HzA2aOtWZnP47iBs5os",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"page":1`,
				`"perPage":30`,
				`"totalItems":0`,
				`"totalPages":0`,
				`"items":[]`,
			},
			ExpectedEvents: map[string]int{
				"*":                    0,
				"OnRecordsListRequest": 1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordCrudOTPView(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodGet,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "non-owner",
			Method: http.MethodGet,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// clients, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJ2ODUxcTRyNzkwcmhrbmwiLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiZ2szOTBxZWdzNHk0N3duIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.xCGkWuACPNAEUBLQVK4KKp72HzA2aOtWZnP47iBs5os",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "owner",
			Method: http.MethodGet,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// users, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"id":"user1_0"`},
			ExpectedEvents: map[string]int{
				"*":                   0,
				"OnRecordViewRequest": 1,
				"OnRecordEnrich":      1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordCrudOTPDelete(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodDelete,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "non-owner auth",
			Method: http.MethodDelete,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// clients, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJ2ODUxcTRyNzkwcmhrbmwiLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiZ2szOTBxZWdzNHk0N3duIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.xCGkWuACPNAEUBLQVK4KKp72HzA2aOtWZnP47iBs5os",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "owner regular auth",
			Method: http.MethodDelete,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// users, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "superusers auth",
			Method: http.MethodDelete,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// superusers, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordDeleteRequest":      1,
				"OnModelDelete":              1,
				"OnModelDeleteExecute":       1,
				"OnModelAfterDeleteSuccess":  1,
				"OnRecordDelete":             1,
				"OnRecordDeleteExecute":      1,
				"OnRecordAfterDeleteSuccess": 1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordCrudOTPCreate(t *testing.T) {
	t.Parallel()

	body := func() *strings.Reader {
		return strings.NewReader(`{
			"recordRef":     "4q1xlclmfloku33",
			"collectionRef": "_hz_users_auth_",
			"password":      "abc"
		}`)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodPost,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records",
			Body:   body(),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "owner regular auth",
			Method: http.MethodPost,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records",
			Headers: map[string]string{
				// users, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			Body: body(),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "superusers auth",
			Method: http.MethodPost,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records",
			Headers: map[string]string{
				// superusers, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			Body: body(),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedContent: []string{
				`"recordRef":"4q1xlclmfloku33"`,
			},
			ExpectedStatus: 200,
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordCreateRequest":      1,
				"OnRecordEnrich":             1,
				"OnModelCreate":              1,
				"OnModelCreateExecute":       1,
				"OnModelAfterCreateSuccess":  1,
				"OnModelValidate":            1,
				"OnRecordCreate":             1,
				"OnRecordCreateExecute":      1,
				"OnRecordAfterCreateSuccess": 1,
				"OnRecordValidate":           1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordCrudOTPUpdate(t *testing.T) {
	t.Parallel()

	body := func() *strings.Reader {
		return strings.NewReader(`{
			"password":"abc"
		}`)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodPatch,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Body:   body(),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "owner regular auth",
			Method: http.MethodPatch,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// users, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			Body: body(),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "superusers auth",
			Method: http.MethodPatch,
			URL:    "/api/collections/" + core.CollectionNameOTPs + "/records/user1_0",
			Headers: map[string]string{
				// superusers, test@example.com
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			Body: body(),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := tests.StubOTPRecords(app); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedContent: []string{
				`"id":"user1_0"`,
			},
			ExpectedStatus: 200,
			ExpectedEvents: map[string]int{
				"*":                          0,
				"OnRecordUpdateRequest":      1,
				"OnRecordEnrich":             1,
				"OnModelUpdate":              1,
				"OnModelUpdateExecute":       1,
				"OnModelAfterUpdateSuccess":  1,
				"OnModelValidate":            1,
				"OnRecordUpdate":             1,
				"OnRecordUpdateExecute":      1,
				"OnRecordAfterUpdateSuccess": 1,
				"OnRecordValidate":           1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
