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

// param is one parameter of a raw query: the separator that preceded it (0 for
// the first), the name it unescapes to, and the bytes exactly as they arrived.
type param struct {
	sep  byte
	name string
	raw  string
}

// params splits a raw query on `;` as well as `&`. Go stopped treating `;` as a
// separator, so `?a=1;key=pk-` carries no key by Go's reading while a server
// that still splits on it reads one — and these answers decide what is
// forwarded, so they have to see every parameter the far side might.
func params(raw string) []param {
	if raw == "" {
		return nil
	}

	var out []param
	sep := byte(0)

	for {
		part, next := raw, byte(0)
		if i := strings.IndexAny(raw, "&;"); i >= 0 {
			part, next, raw = raw[:i], raw[i], raw[i+1:]
		} else {
			raw = ""
		}

		name, _, _ := strings.Cut(part, "=")
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}
		out = append(out, param{sep: sep, name: name, raw: part})

		if next == 0 {
			return out
		}
		sep = next
	}
}

// keyAt is where a query presents its credential: the index of the FIRST
// parameter named `key`, and -1 for a query presenting none.
//
// One definition, read by the answer about what ARRIVED and by the answer about
// what is FORWARDED. Two definitions is how a request comes to be judged on one
// value and read upstream on another.
func keyAt(ps []param) int {
	for i, p := range ps {
		if p.name == "key" {
			return i
		}
	}

	return -1
}

// queryKey is the credential a raw query presents.
func queryKey(raw string) string {
	ps := params(raw)

	i := keyAt(ps)
	if i < 0 {
		return ""
	}

	_, value, _ := strings.Cut(ps[i].raw, "=")
	value, err := url.QueryUnescape(value)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(value)
}

// Query is the query a request may be forwarded with: its own, with every `key`
// parameter after the first removed.
//
// A proxy forwards a query whole, so a request presenting `key` twice is judged
// here on one value and read upstream on the other — whichever a server that
// takes the last occurrence would take. `?key=nonsense&key=pk-` was judged on
// nonsense, passed as carrying no publishable key, and handed the issuer the
// publishable key. What was judged is what is forwarded.
//
// Everything else arrives as it came, separators included: whether `a=1;b=2` is
// two parameters or none is the far side's reading to make, and rewriting it
// here would answer that question on its behalf.
func Query(r *http.Request) string {
	raw := r.URL.RawQuery

	ps := params(raw)
	first := keyAt(ps)
	if first < 0 {
		return raw
	}

	var b strings.Builder
	wrote := false
	for i, p := range ps {
		if p.name == "key" && i != first {
			continue
		}
		if wrote && p.sep != 0 {
			b.WriteByte(p.sep)
		}
		b.WriteString(p.raw)
		wrote = true
	}

	return b.String()
}
