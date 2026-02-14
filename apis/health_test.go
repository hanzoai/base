package apis_test

import (
	"net/http"
	"testing"

	"github.com/hanzoai/base/tests"
)

func TestHealthAPI(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:           "GET health status (guest)",
			Method:         http.MethodGet, // automatically matches also HEAD as a side-effect of the Go std mux
			URL:            "/api/health",
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"code":200`,
				`"data":{}`,
			},
			NotExpectedContent: []string{
				"canBackup",
				"realIP",
				"possibleProxyHeader",
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "GET health status (regular user)",
			Method: http.MethodGet,
			URL:    "/api/health",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfaHpfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.jhQ8TO5St_jnNTfceWIaEgdSRTu73NEtR5HPpwYL5Lw",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"code":200`,
				`"data":{}`,
			},
			NotExpectedContent: []string{
				"canBackup",
				"realIP",
				"possibleProxyHeader",
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "GET health status (superuser)",
			Method: http.MethodGet,
			URL:    "/api/health",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"code":200`,
				`"data":{`,
				`"canBackup":true`,
				`"realIP"`,
				`"possibleProxyHeader"`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
