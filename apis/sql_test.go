package apis_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
)

// serveTestJWKS stands up a throwaway JWKS endpoint serving the given public
// key. It mirrors the identity provider Base validates against in production —
// the test app points StoreKeyJWKSURL at it so loadAuthToken takes the IAM
// (JWKS) path, exactly like a real Hanzo IAM deployment.
func serveTestJWKS(t *testing.T, pub *rsa.PublicKey, kid string) *httptest.Server {
	t.Helper()
	jwks := map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"kid": kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestSQLRun exercises the SQL console (/v1/sql) under Base's IAM-native auth.
//
// Auth is a real IAM JWT validated against a JWKS endpoint — there are no
// hardcoded record tokens and no rows written to Base auth collections.
// Superuser privilege is an IAM claim (`isAdmin`), not a stored `_superusers`
// row: the JWKS path mints an ephemeral, unpersisted record so the gate can
// evaluate it. IAM is the user store; Base only validates.
func TestSQLRun(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "test-iam-key"
	jwks := serveTestJWKS(t, &key.PublicKey, kid)

	mint := func(claims jwt.MapClaims) string {
		now := time.Now()
		claims["iat"] = now.Unix()
		claims["exp"] = now.Add(time.Hour).Unix()
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		signed, signErr := tok.SignedString(key)
		if signErr != nil {
			t.Fatalf("mint token: %v", signErr)
		}
		return signed
	}

	// Superuser: IAM token carrying the admin claim. Regular: a plain IAM
	// identity with no admin claim.
	superuserToken := mint(jwt.MapClaims{"sub": "iam-admin", "email": "admin@example.com", "isAdmin": true})
	regularToken := mint(jwt.MapClaims{"sub": "iam-regular", "email": "user@example.com"})

	// Point the app at the JWKS endpoint and make IAM the exclusive auth
	// source, the production posture once the platform plugin registers.
	useIAM := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
		app.Store().Set(apis.StoreKeyJWKSURL, jwks.URL)
	}
	asSuperuser := map[string]string{"Authorization": superuserToken}

	scenarios := []tests.ApiScenario{
		{
			Name:            "guest",
			Method:          http.MethodPost,
			URL:             "/v1/sql",
			Body:            strings.NewReader(`{"query":"select 1"}`),
			BeforeTestFunc:  useIAM,
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:            "regular user (no admin claim)",
			Method:          http.MethodPost,
			URL:             "/v1/sql",
			Body:            strings.NewReader(`{"query":"select 1"}`),
			Headers:         map[string]string{"Authorization": regularToken},
			BeforeTestFunc:  useIAM,
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
			ExpectedEvents:  map[string]int{"*": 0},
		},
		{
			Name:           "superuser (IAM admin claim)",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"select 1"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"execTime":`,
				`"affectedRows":0`,
				`"columns":[{"name":"1","type":"","nullable":true}]`,
				`"rows":[["1"]]`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "empty query",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":""}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"query":{`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "invalid query",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"invalid"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{}`,
				`Raw error:`,
				`SQL logic error`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "query with length above the limit",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"` + strings.Repeat("a", 5001) + `"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{`,
				`"query":{`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "query with length equal to the limit",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"select '` + strings.Repeat("a", 4985) + `' as id"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"execTime":`,
				`"affectedRows":0`,
				`"columns":[{"name":"id","type":"","nullable":true}]`,
				`"rows":[["aaa`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "single write query",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"create table test_sql_table(id int primary key)"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if !app.HasTable("test_sql_table") {
					t.Fatalf("Missing expected new %q table", "test_sql_table")
				}
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"execTime":`,
				// hanzoai/sqlite's pure-Go (no-CGO) backend reports
				// RowsAffected()=1 for DDL; the handler passes the driver
				// value through faithfully rather than special-casing DDL.
				`"affectedRows":1`,
				`"columns":[]`,
				`"rows":[]`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "multiple write queries",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"create table test_sql_table(id int primary key);insert into test_sql_table(id)VALUES(1)"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				var total int
				err := app.DB().NewQuery("select count(*) from test_sql_table").Row(&total)
				if err != nil {
					t.Fatal(err)
				}

				if total != 1 {
					t.Fatalf("Expected exactly 1 row, found: %d", total)
				}
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"execTime":`,
				`"affectedRows":1`,
				`"columns":[]`,
				`"rows":[]`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "multiple write queries (transaction rollback)",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"create table test_sql_table(id int primary key);insert into test_sql_table(id)VALUES(1);invalid"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if app.HasTable("test_sql_table") {
					t.Fatalf("Expected table %q to not be created", "test_sql_table")
				}
			},
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"data":{}`,
				`Raw error:`,
				`SQL logic error`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:           "multiple read queries",
			Method:         http.MethodPost,
			URL:            "/v1/sql",
			Body:           strings.NewReader(`{"query":"select 1;select 2"}`),
			Headers:        asSuperuser,
			BeforeTestFunc: useIAM,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"execTime":`,
				`"affectedRows":0`,
				// only the result of the last query should be returned
				`"columns":[{"name":"2","type":"","nullable":true}]`,
				`"rows":[["2"]]`,
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
