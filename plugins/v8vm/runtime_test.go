//go:build v8vm
// +build v8vm

package v8vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/plugins/extruntime"
)

// writeExt builds a minimal extension dir on disk and returns its path.
func writeExt(t *testing.T, name, src string, exports ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(
		`{"name":"`+name+`","version":"0.1.0","runtime":"v8go","module":"mod.js","exports":`+jsArr(exports)+`}`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func jsArr(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(v)
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}

func TestRuntime_Capabilities(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	if rt.Name() != "v8go" {
		t.Fatalf("Name = %q, want v8go", rt.Name())
	}
	caps := rt.Capabilities()
	if !caps.Cgo || !caps.HardSandbox || !caps.SupportsAbort {
		t.Fatalf("unexpected caps: %+v", caps)
	}
	if len(caps.AcceptsLanguages) != 1 || caps.AcceptsLanguages[0] != "js" {
		t.Fatalf("languages = %v, want [js]", caps.AcceptsLanguages)
	}
}

func TestModule_Invoke(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "echo", `
globalThis.echo = function(p) { return { in: p, ok: true }; };
globalThis.upper = function(p) { return p.s.toUpperCase(); };
globalThis.sum = function(p) { return p.a + p.b; };
globalThis.nothing = function() { /* returns undefined */ };
`, "echo", "upper", "sum", "nothing")

	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	cases := []struct {
		name string
		fn   string
		in   string
		want string
	}{
		{"echo-object", "echo", `{"x":1}`, `{"in":{"x":1},"ok":true}`},
		{"upper-string", "upper", `{"s":"hello"}`, `"HELLO"`},
		{"sum-numbers", "sum", `{"a":2,"b":3}`, `5`},
		{"undefined-result", "nothing", `null`, `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mod.Invoke(context.Background(), tc.fn, []byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestModule_UnknownFunction(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "ext", `globalThis.exists = function() { return 1; };`, "exists")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if _, err := mod.Invoke(context.Background(), "missing", []byte("null")); !errors.Is(err, extruntime.ErrUnknownFn) {
		t.Fatalf("err = %v, want ErrUnknownFn", err)
	}
}

func TestModule_CancelTerminates(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "spin", `globalThis.spin = function() { while (true) { /* tight loop */ } };`, "spin")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = mod.Invoke(ctx, "spin", []byte("null"))
	dur := time.Since(start)
	if err == nil {
		t.Fatal("expected error from cancelled infinite loop")
	}
	if dur > 5*time.Second {
		t.Fatalf("TerminateExecution did not fire in time: %v", dur)
	}
}

func TestModule_HeapBomb(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	// Allocate ~200MB string to blow past default heap; V8 throws
	// RangeError. We just need the call to error, not panic.
	dir := writeExt(t, "bomb", `
globalThis.bomb = function() {
  var s = "x";
  for (var i = 0; i < 30; i++) { s = s + s; } // 2^30 chars
  return s.length;
};`, "bomb")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = mod.Invoke(ctx, "bomb", []byte("null"))
	if err == nil {
		t.Fatal("expected OOM / range error from heap bomb")
	}
}

func TestModule_PoolReuse(t *testing.T) {
	// Pool size of 2; 10 invocations should reuse contexts (we check
	// the pool never grows past poolSize).
	t.Setenv(envPoolSize, "2")
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "p", `globalThis.id = function(p) { return p; };`, "id")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	for i := 0; i < 10; i++ {
		out, err := mod.Invoke(context.Background(), "id", []byte(`{"i":1}`))
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != `{"i":1}` {
			t.Fatalf("got %s", out)
		}
	}

	mImpl, ok := mod.(*module)
	if !ok {
		t.Fatal("module is not *module")
	}
	mImpl.mu.Lock()
	n := len(mImpl.pool)
	mImpl.mu.Unlock()
	if n > 2 {
		t.Fatalf("pool grew past cap: %d", n)
	}
}

func TestRuntime_LoadRejectsWrongRuntime(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.json"),
		[]byte(`{"name":"x","runtime":"wazero","module":"x.wasm"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := rt.Load(context.Background(), dir)
	if !errors.Is(err, extruntime.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestModule_ClosedRejects(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "c", `globalThis.f = function() { return 1; };`, "f")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := mod.Invoke(context.Background(), "f", []byte("null")); !errors.Is(err, extruntime.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}
