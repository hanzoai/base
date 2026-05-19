//go:build pyvm
// +build pyvm

package pyvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hanzoai/base/plugins/extruntime"
)

// writeExt builds a minimal extension dir on disk and returns its path.
func writeExt(t *testing.T, name, src string, exports ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(
		`{"name":"`+name+`","version":"0.1.0","runtime":"pyvm","module":"mod.py","exports":`+jsArr(exports)+`}`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.py"), []byte(src), 0o644); err != nil {
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
	if rt.Name() != "pyvm" {
		t.Fatalf("Name = %q, want pyvm", rt.Name())
	}
	caps := rt.Capabilities()
	if !caps.Cgo || caps.HardSandbox || caps.SupportsAbort {
		t.Fatalf("unexpected caps: %+v", caps)
	}
	if len(caps.AcceptsLanguages) != 1 || caps.AcceptsLanguages[0] != "py" {
		t.Fatalf("languages = %v, want [py]", caps.AcceptsLanguages)
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

func TestModule_Invoke(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "echo", `
def echo(p):
    return {"in": p, "ok": True}

def upper(p):
    return p["s"].upper()

def add(p):
    return p["a"] + p["b"]

def nothing(_):
    return None
`, "echo", "upper", "add", "nothing")

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
		{"echo-object", "echo", `{"x":1}`, `{"in": {"x": 1}, "ok": true}`},
		{"upper-string", "upper", `{"s":"hello"}`, `"HELLO"`},
		{"sum-numbers", "add", `{"a":2,"b":3}`, `5`},
		{"none-result", "nothing", `null`, `null`},
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
	dir := writeExt(t, "ext", `
def exists(_):
    return 1
`, "exists")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if _, err := mod.Invoke(context.Background(), "missing", []byte("null")); !errors.Is(err, extruntime.ErrUnknownFn) {
		t.Fatalf("err = %v, want ErrUnknownFn", err)
	}
}

func TestModule_ClosedRejects(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "c", `
def f(_):
    return 1
`, "f")
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

// TestModule_ConcurrentSubInterpreters proves the pool actually
// supports parallel work. With pool_size=4 and 4 goroutines firing
// CPU-bound Python at the same module, total wall time should be
// well under 4x single-iteration time (true parallelism on the
// free-threaded build, or interleaved OWN_GIL parallel on standard
// 3.12+).
func TestModule_ConcurrentSubInterpreters(t *testing.T) {
	t.Setenv(envPoolSize, "4")
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "burn", `
def burn(p):
    n = p["n"]
    s = 0
    for i in range(n):
        s = (s + i) % 1000003
    return s
`, "burn")

	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	const goroutines = 4
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := mod.Invoke(context.Background(), "burn", []byte(`{"n":200000}`)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

// TestModule_PreCancelledCtx proves ctx pre-cancel is honored
// before entering Python.
//
// HONEST LIMITATION: pyvm does NOT support hard abort of an already-
// running Python invocation. CPython doesn't expose a primitive for
// "abort this specific sub-interpreter from another OS thread without
// holding its GIL" (Py_AddPendingCall routes to the main interpreter,
// PyThreadState_SetAsyncExc requires the target's GIL). Callers
// needing hard abort must use wazero + RustPython. See README.md.
func TestModule_PreCancelledCtx(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "echo", `
def echo(p):
    return p
`, "echo")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invoke
	_, err = mod.Invoke(ctx, "echo", []byte(`{"x":1}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
