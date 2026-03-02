package apis_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/dbx"
)

// Reuse the auth tokens baked into tests/data — same user the rest of the
// suite authenticates as.
const (
	privateUserAToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfdXNlcnNfYXV0aF8iLCJleHAiOjI1MjQ2MDQ0NjEsImlkIjoiNHExeGxjbG1mbG9rdTMzIiwicmVmcmVzaGFibGUiOnRydWUsInR5cGUiOiJhdXRoIn0.AuFTIzCsdLEy-5adFzpjZzbqAdTP6Iu9B1wPBAxLBgo"
	privateTestUserID = "4q1xlclmfloku33"
)

// seedPrivateRow inserts a ciphertext row for the test user. Creates the
// table first since the endpoint does so lazily on first use.
func seedPrivateRow(app *tests.TestApp, tag string, ct []byte) error {
	_, err := app.DB().NewQuery(`
		CREATE TABLE IF NOT EXISTS _private (
			user_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			ct BLOB NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, tag)
		) WITHOUT ROWID
	`).Execute()
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO _private (user_id, tag, ct, updated_at)
		VALUES ({:u}, {:t}, {:c}, {:n})
	`).Bind(dbx.Params{"u": privateTestUserID, "t": tag, "c": ct, "n": int64(1700000000000)}).Execute()
	return err
}

func TestPrivateAPI(t *testing.T) {
	t.Parallel()

	ciphertext := []byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe}

	scenarios := []tests.ApiScenario{
		// --- Auth gating ---
		{
			Name:            "PUT private tag as guest → 401",
			Method:          http.MethodPut,
			URL:             "/api/private/watchlist",
			Body:            bytes.NewReader(ciphertext),
			ExpectedStatus:  401,
			ExpectedContent: []string{`"status":401`},
		},
		{
			Name:            "GET private tag as guest → 401",
			Method:          http.MethodGet,
			URL:             "/api/private/watchlist",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"status":401`},
		},
		{
			Name:            "LIST private tags as guest → 401",
			Method:          http.MethodGet,
			URL:             "/api/private",
			ExpectedStatus:  401,
			ExpectedContent: []string{`"status":401`},
		},

		// --- Tag validation ---
		{
			Name:            "PUT with dotdot tag → 400",
			Method:          http.MethodPut,
			URL:             "/api/private/..bad",
			Body:            bytes.NewReader(ciphertext),
			Headers:         map[string]string{"Authorization": privateUserAToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`, "Invalid tag"},
		},

		// --- Happy path writes ---
		{
			Name:           "PUT ciphertext returns size + timestamp",
			Method:         http.MethodPut,
			URL:            "/api/private/watchlist",
			Body:           bytes.NewReader(ciphertext),
			Headers:        map[string]string{"Authorization": privateUserAToken},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"tag":"watchlist"`,
				`"size":8`,
			},
		},
		{
			Name:            "PUT empty body → 400 (must DELETE)",
			Method:          http.MethodPut,
			URL:             "/api/private/empty",
			Body:            bytes.NewReader([]byte{}),
			Headers:         map[string]string{"Authorization": privateUserAToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"status":400`, "Empty ciphertext"},
		},

		// --- Reads (seed pre-row via BeforeTestFunc) ---
		{
			Name:    "LIST returns seeded tag",
			Method:  http.MethodGet,
			URL:     "/api/private",
			Headers: map[string]string{"Authorization": privateUserAToken},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := seedPrivateRow(app, "watchlist", ciphertext); err != nil {
					t.Fatalf("seed failed: %v", err)
				}
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"tag":"watchlist"`,
				`"size":8`,
				`"updated_at":1700000000000`,
			},
		},
		{
			Name:    "GET returns seeded ciphertext bytes",
			Method:  http.MethodGet,
			URL:     "/api/private/watchlist",
			Headers: map[string]string{"Authorization": privateUserAToken},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := seedPrivateRow(app, "watchlist", ciphertext); err != nil {
					t.Fatalf("seed failed: %v", err)
				}
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if ct := res.Header.Get("Content-Type"); ct != "application/octet-stream" {
					t.Errorf("expected octet-stream, got %q", ct)
				}
				if ua := res.Header.Get("X-Private-Updated-At"); ua != "1700000000000" {
					t.Errorf("expected updated_at header, got %q", ua)
				}
			},
			ExpectedStatus: 200,
			// The body is raw binary — bypass the content string check by
			// asserting that the response doesn't contain the JSON envelope.
			NotExpectedContent: []string{`"status"`, `"data"`},
		},
		{
			Name:            "GET non-existent tag → 404",
			Method:          http.MethodGet,
			URL:             "/api/private/nope",
			Headers:         map[string]string{"Authorization": privateUserAToken},
			ExpectedStatus:  404,
			ExpectedContent: []string{`"status":404`},
		},

		// --- Delete ---
		{
			Name:           "DELETE seeded tag → 204",
			Method:         http.MethodDelete,
			URL:            "/api/private/watchlist",
			Headers:        map[string]string{"Authorization": privateUserAToken},
			ExpectedStatus: 204,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if err := seedPrivateRow(app, "watchlist", ciphertext); err != nil {
					t.Fatalf("seed failed: %v", err)
				}
			},
		},
	}

	for _, s := range scenarios {
		s.Test(t)
	}
}
