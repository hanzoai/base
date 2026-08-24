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

// TestCredentialReadsPastAnUnreadableValue pins that a repeated header does not
// hide the credential. Both values are forwarded whole, so a value this side
// cannot read must not stop it reading the next one.
func TestCredentialReadsPastAnUnreadableValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/anywhere", nil)
	r.Header.Add("Authorization", "Basic dXNlcjpwYXNz")
	r.Header.Add("Authorization", "Bearer tok")

	if got := Credential(r); got != "tok" {
		t.Errorf("Credential = %q, want %q", got, "tok")
	}
}

// TestQueryForwardsTheCredentialItWasJudgedOn pins that the query a proxy may
// forward presents the same credential this side read, and only that one.
//
// A query presenting `key` twice is judged here by the first and read by a
// last-occurrence server by the second, so `?key=nonsense&key=pk-` passed as
// carrying no publishable key and handed one on.
func TestQueryForwardsTheCredentialItWasJudgedOn(t *testing.T) {
	for _, c := range []struct{ how, query, want, judged string }{
		{"one key", "?key=pk-x", "key=pk-x", "pk-x"},
		{"a decoy in front", "?key=nonsense&key=pk-x", "key=nonsense", "nonsense"},
		{"a decoy after a semicolon", "?key=nonsense;key=pk-x", "key=nonsense", "nonsense"},
		{"a decoy under an escaped name", "?key=nonsense&k%65y=pk-x", "key=nonsense", "nonsense"},
		{"an escaped name first", "?k%65y=pk-x&key=nonsense", "k%65y=pk-x", "pk-x"},
		{"a stronger key in front", "?key=sk-y&key=pk-x", "key=sk-y", "sk-y"},
		{"three of them", "?key=a&key=b&key=c", "key=a", "a"},
		{"the rest of the query kept", "?a=1&key=nonsense&b=2&key=pk-x", "a=1&key=nonsense&b=2", "nonsense"},
		{"no key at all", "?a=1&b=2", "a=1&b=2", ""},
		{"semicolons left alone", "?a=1;b=2", "a=1;b=2", ""},
		{"a leading separator kept", "?&key=pk-x", "&key=pk-x", "pk-x"},
		{"a trailing separator kept", "?key=pk-x&", "key=pk-x&", "pk-x"},
		{"nothing at all", "", "", ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/anywhere"+c.query, nil)

		if got := Query(r); got != c.want {
			t.Errorf("%s: Query = %q, want %q", c.how, got, c.want)
		}
		if got := Credential(r); got != c.judged {
			t.Errorf("%s: Credential = %q, want %q", c.how, got, c.judged)
		}

		// The forwarded query presents the credential that was judged. Read it
		// back the way the far side would: one reader, both ends.
		forwarded := httptest.NewRequest(http.MethodGet, "/anywhere?"+Query(r), nil)
		if got := Credential(forwarded); got != c.judged {
			t.Errorf("%s: the forwarded query presents %q, judged on %q", c.how, got, c.judged)
		}
	}
}
