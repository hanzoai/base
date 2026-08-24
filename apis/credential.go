package apis

import (
	"net/http"
	"net/url"
	"strings"
)

// credentialHeaders are the headers a credential arrives in, most canonical
// first. Authorization is the one a client should send; X-Authorization is the
// alias for a proxy that consumes Authorization; X-Auth-Token is what older
// clients send; X-API-Key is what a machine sends.
var credentialHeaders = [...]string{
	"Authorization",
	"X-Authorization",
	"X-Auth-Token",
	"X-API-Key",
}

// Credential is the credential a request carries, in whichever of the spellings
// this process accepts: a Bearer token or a bare token in any of
// [credentialHeaders], and otherwise the `key` query parameter.
//
// One reader, read by everything. A rule that decides what a credential may
// REACH and a door that RESOLVES it agree only if they read the same bytes, and
// three readers that each accepted a different subset of the spellings meant a
// key one of them refused was a key another handed to an upstream: `Bearer pk-`
// was refused while `bearer pk-`, a bare `pk-`, the same token under three other
// header names and a `;`-separated query all went through. Whether a credential
// arrived is a property of the request, so it is answered in one place from the
// request.
//
// A publishable key travels in the query — that is what makes the page source
// the credential — so the query spelling is read here too, and the headers win
// where a request carries both.
func Credential(r *http.Request) string {
	for _, name := range credentialHeaders {
		// Every value, not the first. A header may repeat, and a repeat is
		// forwarded whole — so reading only the first lets a request put
		// something unreadable in front of the credential it means to use and
		// have this side see no credential while the far side sees one.
		for _, value := range r.Header.Values(name) {
			if token := bearer(value); token != "" {
				return token
			}
		}
	}

	return queryKey(r.URL.RawQuery)
}

// bearer is the token a header value presents: what follows a Bearer scheme, or
// the whole value where it names no scheme at all, which is how the JS SDK
// sends one.
//
// The scheme is matched without regard to case and separated by any run of
// spaces or tabs, because `bearer pk-`, `Bearer  pk-` and "Bearer\tpk-" are one
// credential to every server that will receive them.
//
// A value naming any OTHER scheme presents no token. Handing back `Basic
// dXNlcjpwYXNz` whole would put a password blob where a token belongs, and the
// caller would then compare key prefixes against it and match a query parameter
// on it.
func bearer(value string) string {
	value = strings.TrimSpace(value)

	i := strings.IndexAny(value, " \t")
	if i < 0 {
		return value
	}
	if !strings.EqualFold(value[:i], "Bearer") {
		return ""
	}

	return strings.TrimSpace(value[i:])
}

// queryKey is the `key` parameter of a raw query string.
//
// It splits on `;` as well as `&` and unescapes both halves. Go stopped
// treating `;` as a separator, so `?a=1;key=pk-` carries no key by Go's reading
// while a server that still splits on it reads one — and this answer decides
// what gets FORWARDED, so it has to see every parameter the far side might.
func queryKey(raw string) string {
	for raw != "" {
		part := raw
		if i := strings.IndexAny(raw, "&;"); i >= 0 {
			part, raw = raw[:i], raw[i+1:]
		} else {
			raw = ""
		}

		name, value, _ := strings.Cut(part, "=")
		if name, err := url.QueryUnescape(name); err != nil || name != "key" {
			continue
		}
		if value, err := url.QueryUnescape(value); err == nil {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}

	return ""
}
