package apis_test

import (
	"net/http"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func TestRecordAuthMethodsList(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:            "missing collection",
			Method:          http.MethodGet,
			URL:             "/v1/collections/missing/auth-methods",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "non auth collection",
			Method:          http.MethodGet,
			URL:             "/v1/collections/demo1/auth-methods",
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			// the collection stores a disabled oauth2 config, and it makes no
			// difference: auth is IAM, so every auth collection advertises IAM.
			Name:           "auth collection, IAM unconfigured",
			Method:         http.MethodGet,
			URL:            "/v1/collections/nologin/auth-methods",
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"oauth2":{`,
				`"providers":[{`,
				`"name":"iam"`,
				`"enabled":true`,
				`"authURL":""`, // nowhere to send anyone until IAM is named
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "auth collection, IAM configured",
			Method: http.MethodGet,
			URL:    "/v1/collections/users/auth-methods",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyJWKSURL, "https://iam.example.com/v1/iam/.well-known/jwks")
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"name":"iam"`,
				`"state":`,
				`"displayName":`,
				`"codeVerifier":`,
				`"codeChallenge":`,
				`"codeChallengeMethod":"S256"`,
				`"authURL":"https://iam.example.com/v1/iam/oauth/authorize?response_type=code`,
				`redirect_uri="`, // ensures that the redirect_uri is the last url param
			},
			ExpectedEvents: map[string]int{"*": 0},
		},

		// rate limit checks
		// -----------------------------------------------------------
		{
			Name:   "RateLimit rule - nologin:listAuthMethods",
			Method: http.MethodGet,
			URL:    "/v1/collections/nologin/auth-methods",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 100, Label: "*:listAuthMethods"},
					{MaxRequests: 0, Label: "nologin:listAuthMethods"},
				}
			},
			ExpectedStatus:  429,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "RateLimit rule - *:listAuthMethods",
			Method: http.MethodGet,
			URL:    "/v1/collections/nologin/auth-methods",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().RateLimits.Enabled = true
				app.Settings().RateLimits.Rules = []core.RateLimitRule{
					{MaxRequests: 100, Label: "abc"},
					{MaxRequests: 0, Label: "*:listAuthMethods"},
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
