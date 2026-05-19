// Package nativego is the native-Go extbench fixture. It registers a single
// `validate` function under the "validate-email" extension name, matching
// the shared semantics implemented identically across all 5 runtime
// fixtures (see goja-js/validate.js for the canonical reference):
//
//   in:    {"email": "Foo@Example.COM ", "age": 25}
//   ok:    {"ok": true, "email": "foo@example.com", "age": 25}
//   err:   {"ok": false, "error": "input must be an object"}
//   err:   {"ok": false, "error": "email required"}
//   err:   {"ok": false, "error": "age out of range"}
//   err:   {"ok": false, "error": "email shape"}
//   err:   {"ok": false, "error": "email domain"}
package nativego

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/hanzoai/base/plugins/extruntime"
)

type okOut struct {
	OK    bool   `json:"ok"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

type errOut struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func errJSON(msg string) ([]byte, error) {
	return json.Marshal(errOut{OK: false, Error: msg})
}

func validate(_ context.Context, payload []byte) ([]byte, error) {
	// "input must be an object" is the same gate the JS fixtures apply
	// for null / non-object / unparsable input.
	trim := bytes.TrimSpace(payload)
	if len(trim) == 0 || trim[0] != '{' {
		return errJSON("input must be an object")
	}
	// json.Number lets us preserve "is this actually a number?" vs
	// "string that happens to parse as int" without losing precision.
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return errJSON("input must be an object")
	}

	emailRaw, ok := raw["email"]
	if !ok {
		return errJSON("email required")
	}
	email, ok := emailRaw.(string)
	if !ok || len(email) == 0 {
		return errJSON("email required")
	}

	ageRaw, ok := raw["age"]
	if !ok {
		return errJSON("age out of range")
	}
	num, ok := ageRaw.(json.Number)
	if !ok {
		return errJSON("age out of range")
	}
	// JS `typeof age !== "number"` rejects NaN-like strings; json.Number
	// only appears when the source was a JSON number, so parseability
	// here is sufficient. Use float to mirror JS's single number type
	// (covers 25 and 25.0 identically); reject if not finite int 0..150.
	f, err := num.Float64()
	if err != nil || f < 0 || f > 150 {
		return errJSON("age out of range")
	}
	age := int(f)

	normalized := strings.ToLower(strings.TrimSpace(email))
	at := strings.Index(normalized, "@")
	if at <= 0 || at == len(normalized)-1 {
		return errJSON("email shape")
	}
	domain := normalized[at+1:]
	if !strings.Contains(domain, ".") {
		return errJSON("email domain")
	}
	return json.Marshal(okOut{OK: true, Email: normalized, Age: age})
}

func init() {
	extruntime.RegisterNative("validate-email", "validate", validate)
}
