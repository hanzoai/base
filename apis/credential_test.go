package apis

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCredentialReadsEverySpelling pins that one token is one credential
// however it was written.
//
// A reader that accepted only the canonical spelling did not answer "no
// credential" for the rest — it answered "no credential" to the RULE while the
// door beside it, and the upstream past that, read the token fine.
func TestCredentialReadsEverySpelling(t *testing.T) {
	for _, c := range []struct {
		how     string
		headers map[string]string
		query   string
		want    string
	}{
		{"a Bearer token", map[string]string{"Authorization": "Bearer tok"}, "", "tok"},
		{"a lowercase scheme", map[string]string{"Authorization": "bearer tok"}, "", "tok"},
		{"an uppercase scheme", map[string]string{"Authorization": "BEARER tok"}, "", "tok"},
		{"two spaces", map[string]string{"Authorization": "Bearer  tok"}, "", "tok"},
		{"a tab", map[string]string{"Authorization": "Bearer\ttok"}, "", "tok"},
		{"trailing space", map[string]string{"Authorization": " Bearer tok "}, "", "tok"},
		{"a bare token", map[string]string{"Authorization": "tok"}, "", "tok"},
		{"the alias header", map[string]string{"X-Authorization": "Bearer tok"}, "", "tok"},
		{"the alias header bare", map[string]string{"X-Authorization": "tok"}, "", "tok"},
		{"the legacy header", map[string]string{"X-Auth-Token": "tok"}, "", "tok"},
		{"the machine header", map[string]string{"X-API-Key": "tok"}, "", "tok"},
		{"the query", nil, "?key=tok", "tok"},
		{"the query after a semicolon", nil, "?a=1;key=tok", "tok"},
		{"the query escaped", nil, "?key=t%6fk", "tok"},
		{"the query name escaped", nil, "?k%65y=tok", "tok"},

		// A value naming another scheme is not a token. Handing back the blob
		// whole is what makes `?key=` match on a password.
		{"another scheme", map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}, "", ""},
		{"another scheme over a query", map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}, "?key=tok", "tok"},
		{"another scheme over a header", map[string]string{
			"Authorization": "Basic dXNlcjpwYXNz", "X-API-Key": "tok"}, "", "tok"},

		// The order: the canonical header wins where a request carries several.
		{"both headers", map[string]string{"Authorization": "Bearer tok", "X-API-Key": "other"}, "", "tok"},
		{"a header over a query", map[string]string{"Authorization": "Bearer tok"}, "?key=other", "tok"},

		{"nothing at all", nil, "", ""},
		{"an empty header", map[string]string{"Authorization": ""}, "?key=tok", "tok"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/anywhere"+c.query, nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := Credential(r); got != c.want {
			t.Errorf("%s: Credential = %q, want %q", c.how, got, c.want)
		}
	}
}
