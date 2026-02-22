package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func TestRecordConfirmVerification(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:           "empty data",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/confirm-verification",
			Body:           strings.NewReader(``),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"token":{"code":"validation_required"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:            "invalid data format",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/confirm-verification",
			Body:            strings.NewReader(`{"password`),
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired token",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njg4MTI3MTgsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.BPWlwM7N_2UxQH5UFeZz8lvnuzmoXGm9y7Cgy9DV_7I"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"token":{"code":"validation_invalid_token"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "non-verification token",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njg4MTgxMTgsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwidHlwZSI6InBhc3N3b3JkUmVzZXQifQ.3DdKH9oVdG-lO0p6qpQSeQ1hpZuBFa_CNqz9hPYU60w"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"token":{"code":"validation_invalid_token"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "non auth collection",
			Method: http.MethodPost,
			URL:    "/api/collections/demo1/confirm-verification?expand=rel,missing",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.m1H9Wm8qqnYPmbf6KBQnvFNI2bLLV4UmI-Tvt95G-MA"
			}`),
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "different auth collection",
			Method: http.MethodPost,
			URL:    "/api/collections/clients/confirm-verification?expand=rel,missing",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.m1H9Wm8qqnYPmbf6KBQnvFNI2bLLV4UmI-Tvt95G-MA"
			}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{"token":{"code":"validation_token_collection_mismatch"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "valid token",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.m1H9Wm8qqnYPmbf6KBQnvFNI2bLLV4UmI-Tvt95G-MA"
			}`),
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{
				"*":                                  0,
				"OnRecordConfirmVerificationRequest": 1,
				"OnModelUpdate":                      1,
				"OnModelValidate":                    1,
				"OnModelUpdateExecute":               1,
				"OnModelAfterUpdateSuccess":          1,
				"OnRecordUpdate":                     1,
				"OnRecordValidate":                   1,
				"OnRecordUpdateExecute":              1,
				"OnRecordAfterUpdateSuccess":         1,
			},
		},
		{
			Name:   "valid token (already verified)",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3QyQGV4YW1wbGUuY29tIiwiZXhwIjoxNzY5NDIxMTE4LCJpZCI6Im9hcDY0MGNvdDR5cnUycyIsInR5cGUiOiJ2ZXJpZmljYXRpb24ifQ.8001FsRg9CVSClt4hWU1vAbi9b2wXHLaS8MDBT0-S4o"
			}`),
			ExpectedStatus: 204,
			ExpectedEvents: map[string]int{
				"*":                                  0,
				"OnRecordConfirmVerificationRequest": 1,
			},
		},
		{
			Name:   "valid verification token from a collection without allowed login",
			Method: http.MethodPost,
			URL:    "/api/collections/nologin/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJrcHY3MDlzazJscWJxazgiLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiZGM0OWs2amdlam40MGgzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.4rWr4xE3le0FrCmoEwBHngm1cD0JNUJ9iNrMHoRqNJU"
			}`),
			ExpectedStatus:  204,
			ExpectedContent: []string{},
			ExpectedEvents: map[string]int{
				"*":                                  0,
				"OnRecordConfirmVerificationRequest": 1,
				"OnModelUpdate":                      1,
				"OnModelValidate":                    1,
				"OnModelUpdateExecute":               1,
				"OnModelAfterUpdateSuccess":          1,
				"OnRecordUpdate":                     1,
				"OnRecordValidate":                   1,
				"OnRecordUpdateExecute":              1,
				"OnRecordAfterUpdateSuccess":         1,
			},
		},
		{
			Name:   "OnRecordConfirmVerificationRequest tx body write check",
			Method: http.MethodPost,
			URL:    "/api/collections/users/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.m1H9Wm8qqnYPmbf6KBQnvFNI2bLLV4UmI-Tvt95G-MA"
			}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.OnRecordConfirmVerificationRequest().BindFunc(func(e *core.RecordConfirmVerificationRequestEvent) error {
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
			ExpectedEvents:  map[string]int{"OnRecordConfirmVerificationRequest": 1},
			ExpectedContent: []string{"TX_ERROR"},
		},

		// rate limit checks
		// -----------------------------------------------------------
		{
			Name:   "RateLimit rule - nologin:confirmVerification",
			Method: http.MethodPost,
			URL:    "/api/collections/nologin/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJrcHY3MDlzazJscWJxazgiLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiZGM0OWs2amdlam40MGgzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.4rWr4xE3le0FrCmoEwBHngm1cD0JNUJ9iNrMHoRqNJU"
			}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 100, Label: "*:confirmVerification"},
					{MaxRequests: 0, Label: "nologin:confirmVerification"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "RateLimit rule - *:confirmVerification",
			Method: http.MethodPost,
			URL:    "/api/collections/nologin/confirm-verification",
			Body: strings.NewReader(`{
				"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJrcHY3MDlzazJscWJxazgiLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE3Njk0MjExMTgsImlkIjoiZGM0OWs2amdlam40MGgzIiwidHlwZSI6InZlcmlmaWNhdGlvbiJ9.4rWr4xE3le0FrCmoEwBHngm1cD0JNUJ9iNrMHoRqNJU"
			}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 0, Label: "*:confirmVerification"},
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
