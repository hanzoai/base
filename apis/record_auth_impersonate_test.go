package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/tests"
)

func TestRecordAuthImpersonate(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "unauthorized",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/impersonate/4q1xlclmfloku33",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "authorized as different user",
			Method: http.MethodPost,
			URL:    "/api/collections/users/impersonate/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoib2FwNjQwY290NHlydTJzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.6Mw9-w9F8jFYct0-7PSz9dBP-kPTnRNc2vHtQiAVkDQ",
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "authorized as the same user",
			Method: http.MethodPost,
			URL:    "/api/collections/users/impersonate/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "authorized as superuser",
			Method: http.MethodPost,
			URL:    "/api/collections/users/impersonate/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"id":"4q1xlclmfloku33"`,
				`"record":{`,
			},
			NotExpectedContent: []string{
				// hidden fields should remain hidden even though we are authenticated as superuser
				`"tokenKey"`,
				`"password"`,
			},
			ExpectedEvents: map[string]int{
				"*":                   0,
				"OnRecordAuthRequest": 1,
				"OnRecordEnrich":      1,
			},
		},
		{
			Name:   "authorized as superuser with custom invalid duration",
			Method: http.MethodPost,
			URL:    "/api/collections/users/impersonate/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			Body:           strings.NewReader(`{"duration":-1}`),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"duration":{`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "authorized as superuser with custom valid duration",
			Method: http.MethodPost,
			URL:    "/api/collections/users/impersonate/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			Body:           strings.NewReader(`{"duration":100}`),
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"id":"4q1xlclmfloku33"`,
				`"record":{`,
			},
			ExpectedEvents: map[string]int{
				"*":                   0,
				"OnRecordAuthRequest": 1,
				"OnRecordEnrich":      1,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
