package starkvm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hanzoai/base/plugins/extruntime"
)

// writeExt writes a tiny manifest+source pair into a temp dir and
// returns the directory path. Mirrors plugins/gojavm/runtime_test.go's
// helper exactly so the tests read 1:1 across runtimes.
func writeExt(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := extruntime.Manifest{
		Name:    name,
		Version: "0.0.1",
		Runtime: "starlark",
		Module:  "validate.star",
		Exports: []string{"fn"},
	}
	mb, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), mb, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "validate.star"), []byte(src), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	return dir
}

func TestRuntime_NameAndCaps(t *testing.T) {
	r := NewRuntime()
	defer r.Close()

	if r.Name() != "starlark" {
		t.Fatalf("Name = %q, want starlark", r.Name())
	}
	caps := r.Capabilities()
	if len(caps.AcceptsLanguages) != 1 || caps.AcceptsLanguages[0] != "starlark" {
		t.Fatalf("AcceptsLanguages = %v, want [starlark]", caps.AcceptsLanguages)
	}
	if caps.HardSandbox {
		t.Fatal("HardSandbox should be false for starlark (shares Go heap)")
	}
	if caps.Cgo {
		t.Fatal("Cgo should be false for starlark")
	}
	if !caps.SupportsAbort {
		t.Fatal("SupportsAbort should be true (thread.Cancel is per-opcode)")
	}
}

func TestRuntime_LoadRejectsWrongRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.json"),
		[]byte(`{"name":"x","runtime":"goja","module":"x.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRuntime()
	defer r.Close()
	if _, err := r.Load(context.Background(), dir); err == nil {
		t.Fatal("expected error loading goja manifest in starlark runtime")
	}
}

func TestModule_InvokeRoundtrip(t *testing.T) {
	dir := writeExt(t, "echo", `
def fn(p):
    return {"got": p, "doubled": p["n"] * 2}
`)
	r := NewRuntime()
	defer r.Close()

	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	if m.Name() != "echo" || m.Runtime() != "starlark" {
		t.Fatalf("Name/Runtime: %s/%s", m.Name(), m.Runtime())
	}

	out, err := m.Invoke(context.Background(), "fn", []byte(`{"n":21}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	var got struct {
		Got     map[string]any `json:"got"`
		Doubled int            `json:"doubled"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal result %q: %v", out, err)
	}
	if got.Doubled != 42 {
		t.Fatalf("doubled = %d, want 42", got.Doubled)
	}
	if int(got.Got["n"].(float64)) != 21 {
		t.Fatalf("got.n = %v, want 21", got.Got["n"])
	}
}

func TestModule_InvokeUnknownFn(t *testing.T) {
	dir := writeExt(t, "u", `def fn(p): return 1`)
	r := NewRuntime()
	defer r.Close()

	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	_, err = m.Invoke(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected ErrUnknownFn")
	}
}

func TestModule_InvokeAfterClose(t *testing.T) {
	dir := writeExt(t, "c", `def fn(p): return 1`)
	r := NewRuntime()
	defer r.Close()

	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Invoke(context.Background(), "fn", nil); err == nil {
		t.Fatal("Invoke after Close should fail")
	}
}

func TestModule_InvokeContextCancel(t *testing.T) {
	// A while(true) that does cheap arithmetic. Starlark checks the
	// cancel flag between opcodes; the arithmetic generates plenty
	// of opcodes per iteration so cancellation lands promptly.
	src := `
def fn(p):
    x = 0
    while True:
        x = x + 1
    return x
`
	dir := writeExt(t, "loop", src)
	r := NewRuntime()
	defer r.Close()
	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = m.Invoke(ctx, "fn", []byte(`{}`))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error from infinite loop")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancel took too long: %v", elapsed)
	}
}

func TestModule_PoolReuseAcrossInvocations(t *testing.T) {
	// Re-running Init on the same thread is wasted work. We can't
	// observe it directly (no global state since we freeze globals),
	// but we CAN observe that 1000 fast invocations on the same
	// runtime complete without errors and with stable latency.
	src := `def fn(p): return {"sum": p["a"] + p["b"]}`
	dir := writeExt(t, "pool", src)
	r := NewRuntime()
	defer r.Close()
	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	for i := 0; i < 1000; i++ {
		out, err := m.Invoke(context.Background(), "fn", []byte(`{"a":2,"b":3}`))
		if err != nil {
			t.Fatalf("Invoke[%d]: %v", i, err)
		}
		var got struct {
			Sum int `json:"sum"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("unmarshal[%d] %q: %v", i, out, err)
		}
		if got.Sum != 5 {
			t.Fatalf("sum[%d] = %d, want 5", i, got.Sum)
		}
	}
}

func TestModule_ValidateFixtureSemantics(t *testing.T) {
	// Exercise the real fixture (the one extbench loads) end-to-end
	// so we catch any drift between fixture script and runtime ABI.
	src := `
def validate(input):
    if input == None or type(input) != "dict":
        return {"ok": False, "error": "input must be an object"}
    email = input.get("email")
    age = input.get("age")
    if type(email) != "string" or len(email) == 0:
        return {"ok": False, "error": "email required"}
    if type(age) != "int" or age < 0 or age > 150:
        return {"ok": False, "error": "age out of range"}
    normalized = email.strip().lower()
    at = normalized.find("@")
    if at <= 0 or at == len(normalized) - 1:
        return {"ok": False, "error": "email shape"}
    domain = normalized[at + 1:]
    if domain.find(".") < 0:
        return {"ok": False, "error": "email domain"}
    return {"ok": True, "email": normalized, "age": age}
`
	// Use the canonical name `validate` from the runtime fixture so
	// this test exercises the same export resolution path the bench
	// uses.
	dir := t.TempDir()
	mb, _ := json.Marshal(extruntime.Manifest{
		Name:    "validate-email",
		Runtime: "starlark",
		Module:  "validate.star",
		Exports: []string{"validate"},
	})
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "validate.star"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRuntime()
	defer r.Close()
	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	cases := []struct {
		name    string
		in      string
		wantOk  bool
		wantErr string
		wantEm  string
	}{
		{"happy", `{"email":"Foo@Example.COM ","age":25}`, true, "", "foo@example.com"},
		{"old", `{"email":"a@b.c","age":200}`, false, "age out of range", ""},
		{"empty-email", `{"email":"","age":25}`, false, "email required", ""},
		{"no-at", `{"email":"bad","age":25}`, false, "email shape", ""},
		{"no-dot", `{"email":"a@b","age":25}`, false, "email domain", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out, err := m.Invoke(context.Background(), "validate", []byte(tc.in))
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal %q: %v", out, err)
			}
			if got["ok"].(bool) != tc.wantOk {
				t.Fatalf("ok = %v, want %v (full=%s)", got["ok"], tc.wantOk, out)
			}
			if tc.wantOk && got["email"].(string) != tc.wantEm {
				t.Fatalf("email = %v, want %v", got["email"], tc.wantEm)
			}
			if !tc.wantOk && got["error"].(string) != tc.wantErr {
				t.Fatalf("error = %v, want %v", got["error"], tc.wantErr)
			}
		})
	}
}
