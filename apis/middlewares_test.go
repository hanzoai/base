package apis_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

func TestPanicRecover(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "panic from route",
			Method: http.MethodGet,
			URL:    "/my/test",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					panic("123")
				})
			},
			ExpectedStatus:  500,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "panic from middleware",
			Method: http.MethodGet,
			URL:    "/my/test",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(http.StatusOK, "test")
				}).BindFunc(func(e *core.RequestEvent) error {
					panic(123)
				})
			},
			ExpectedStatus:  500,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRequireGuestOnly(t *testing.T) {
	t.Parallel()

	beforeTestFunc := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		e.Router.GET("/my/test", func(e *core.RequestEvent) error {
			return e.String(200, "test123")
		}).Bind(apis.RequireGuestOnly())
	}

	scenarios := []tests.ApiScenario{
		{
			Name:   "valid regular user token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc:  beforeTestFunc,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid superuser auth token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc:  beforeTestFunc,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired/invalid token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjE3Njg3MzA2NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.9iYiBgJP1HRJzqY1WDZxtI9e2CUSkAKNlZmJGiPYQio",
			},
			BeforeTestFunc:  beforeTestFunc,
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "guest",
			Method:          http.MethodGet,
			URL:             "/my/test",
			BeforeTestFunc:  beforeTestFunc,
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRequireAuth(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodGet,
			URL:    "/my/test",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth())
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjE3Njg3MzA2NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.9iYiBgJP1HRJzqY1WDZxtI9e2CUSkAKNlZmJGiPYQio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth())
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "invalid token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6NDkyMjQxNjYwNCwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJ0eXBlIjoiZmlsZSJ9.OuICrgrFGLFWo3zuC0-_Lm3VO3N8U03mOYoDZB7vQp8",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth())
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token with no collection restrictions",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				// regular user
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth())
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
		{
			Name:   "valid record static auth token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				// regular user
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOmZhbHNlLCJ0eXBlIjoiYXV0aCJ9.8LJt8Jz66YTdn5bm_PalFa66lhgdZINXn5EDiVZNV7M",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth())
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
		{
			Name:   "valid record auth token with collection not in the restricted list",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				// superuser
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth("users", "demo1"))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token with collection in the restricted list",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				// superuser
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth("users", core.CollectionNameSuperusers))
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
		{
			Name:   "valid record auth token with Bearer case-insensitive prefix",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				// regular user
				"Authorization": "BeArEr eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireAuth())
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRequireSuperuserAuth(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodGet,
			URL:    "/my/test",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserAuth())
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired/invalid token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MTc2ODczMDM5MywiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.qP2DRKrbLsxafLNzO7ywT4GfykiwCLmDEf5_RmUF99o",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserAuth())
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid regular user auth token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserAuth())
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid superuser auth token",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserAuth())
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRequireSuperuserOrOwnerAuth(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodGet,
			URL:    "/my/test/4q1xlclmfloku33",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth(""))
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired/invalid token",
			Method: http.MethodGet,
			URL:    "/my/test/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MTc2ODczMDM5MywiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.qP2DRKrbLsxafLNzO7ywT4GfykiwCLmDEf5_RmUF99o",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth(""))
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token (different user)",
			Method: http.MethodGet,
			URL:    "/my/test/oap640cot4yru2s",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth(""))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token (owner)",
			Method: http.MethodGet,
			URL:    "/my/test/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth(""))
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
		{
			Name:   "valid record auth token (owner + non-matching custom owner param)",
			Method: http.MethodGet,
			URL:    "/my/test/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth("test"))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token (owner + matching custom owner param)",
			Method: http.MethodGet,
			URL:    "/my/test/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{test}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth("test"))
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
		{
			Name:   "valid superuser auth token",
			Method: http.MethodGet,
			URL:    "/my/test/4q1xlclmfloku33",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth(""))
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRequireSameCollectionContextAuth(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "guest",
			Method: http.MethodGet,
			URL:    "/my/test/_users_auth_",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{collection}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSameCollectionContextAuth(""))
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "expired/invalid token",
			Method: http.MethodGet,
			URL:    "/my/test/_users_auth_",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjE3Njg3MzA2NzIsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.9iYiBgJP1HRJzqY1WDZxtI9e2CUSkAKNlZmJGiPYQio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{collection}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSameCollectionContextAuth(""))
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token (different collection)",
			Method: http.MethodGet,
			URL:    "/my/test/clients",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{collection}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSameCollectionContextAuth(""))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token (same collection)",
			Method: http.MethodGet,
			URL:    "/my/test/_users_auth_",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{collection}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSameCollectionContextAuth(""))
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"test123"},
		},
		{
			Name:   "valid record auth token (non-matching/missing collection param)",
			Method: http.MethodGet,
			URL:    "/my/test/_users_auth_",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{id}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth(""))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "valid record auth token (matching custom collection param)",
			Method: http.MethodGet,
			URL:    "/my/test/_users_auth_",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{test}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSuperuserOrOwnerAuth("test"))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "superuser no exception check",
			Method: http.MethodGet,
			URL:    "/my/test/_users_auth_",
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6MjUyNDYwNDQ2MSwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.CXBf8BazmUeg2RnJW8OEs1UFYF41rbCMOa6YZa4wZio",
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test/{collection}", func(e *core.RequestEvent) error {
					return e.String(200, "test123")
				}).Bind(apis.RequireSameCollectionContextAuth(""))
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestExternalAuthGuard(t *testing.T) {
	t.Parallel()

	scenarios := []tests.ApiScenario{
		{
			Name:   "blocks users auth-with-password when external auth is on",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-password",
			Body:   strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`Direct authentication is disabled`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "allows _superusers auth-with-password when external auth is on",
			Method: http.MethodPost,
			URL:    "/api/collections/_superusers/auth-with-password",
			Body:   strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
			},
			// The superuser endpoint is allowed through the guard.
			// It will fail on actual auth (wrong password) — NOT 403 from the guard.
			ExpectedStatus:     400,
			NotExpectedContent: []string{`Direct authentication is disabled`},
		},
		{
			Name:   "allows users auth-with-password when external auth is off",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-password",
			Body:   strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				// external auth is NOT set (default off)
			},
			// Should NOT get 403 from the guard. Will fail on auth (wrong password).
			ExpectedStatus:     400,
			NotExpectedContent: []string{`Direct authentication is disabled`},
		},
		{
			Name:   "blocks users request-otp when external auth is on",
			Method: http.MethodPost,
			URL:    "/api/collections/users/request-otp",
			Body:   strings.NewReader(`{"email":"test@example.com"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`Direct authentication is disabled`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "blocks users request-password-reset when external auth is on",
			Method: http.MethodPost,
			URL:    "/api/collections/users/request-password-reset",
			Body:   strings.NewReader(`{"email":"test@example.com"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`Direct authentication is disabled`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:   "allows auth-methods when external auth is on",
			Method: http.MethodGet,
			URL:    "/api/collections/users/auth-methods",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"password"`},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestExternalAuthOnlySuperuserFallback(t *testing.T) {
	t.Parallel()

	// superuser local token accepted in external-only mode
	t.Run("superuser token accepted", func(t *testing.T) {
		app, err := tests.NewTestApp()
		if err != nil {
			t.Fatal(err)
		}
		defer app.Cleanup()

		app.Store().Set(apis.StoreKeyExternalAuthOnly, true)

		token, err := tests.GetSuperuserAuthToken(app, tests.TestSuperuserID1)
		if err != nil {
			t.Fatalf("failed to get superuser token: %v", err)
		}

		scenario := tests.ApiScenario{
			Name:   "superuser token in external mode",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": token,
			},
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				return app
			},
			DisableTestAppCleanup: true,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					if e.Auth == nil {
						return e.String(401, "no auth")
					}
					return e.String(200, "auth:"+e.Auth.Collection().Name+":"+e.Auth.Id)
				})
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"auth:_superusers:" + tests.TestSuperuserID1},
		}

		scenario.Test(t)
	})

	// regular user token rejected in external-only mode
	t.Run("regular user token rejected", func(t *testing.T) {
		app, err := tests.NewTestApp()
		if err != nil {
			t.Fatal(err)
		}
		defer app.Cleanup()

		app.Store().Set(apis.StoreKeyExternalAuthOnly, true)

		token, err := tests.GetUserAuthToken(app, "users", tests.TestUserID1)
		if err != nil {
			t.Fatalf("failed to get user token: %v", err)
		}

		scenario := tests.ApiScenario{
			Name:   "regular user token in external mode",
			Method: http.MethodGet,
			URL:    "/my/test",
			Headers: map[string]string{
				"Authorization": token,
			},
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				return app
			},
			DisableTestAppCleanup: true,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				e.Router.GET("/my/test", func(e *core.RequestEvent) error {
					if e.Auth == nil {
						return e.String(401, "no auth")
					}
					return e.String(200, "auth:"+e.Auth.Collection().Name+":"+e.Auth.Id)
				})
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{"no auth"},
		}

		scenario.Test(t)
	})
}
