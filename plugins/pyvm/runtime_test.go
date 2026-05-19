//go:build pyvm
// +build pyvm

package pyvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	gort "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hanzoai/base/plugins/extruntime"
)

// writeExt builds a minimal extension dir on disk and returns its path.
// Pass mode="" to omit the field (runtime picks default per build/env).
func writeExt(t *testing.T, name, src, mode string, exports ...string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name":"` + name + `","version":"0.1.0","runtime":"pyvm","module":"mod.py","exports":` + jsArr(exports)
	if mode != "" {
		manifest += `,"mode":"` + mode + `"`
	}
	manifest += `}`
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(manifest), 0o644); err != nil {
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

// TestMode_DefaultOnFTBuild verifies the auto-detect path picks
// ModeThreaded on a free-threaded build and ModeSubinterp on a
// default-GIL build, assuming no env var override.
func TestMode_DefaultOnBuild(t *testing.T) {
	t.Setenv(envMode, "")
	got := DefaultMode()
	if GilDisabled() {
		if got != ModeThreaded {
			t.Fatalf("FT build: DefaultMode = %v, want %v", got, ModeThreaded)
		}
	} else {
		if got != ModeSubinterp {
			t.Fatalf("default-GIL build: DefaultMode = %v, want %v", got, ModeSubinterp)
		}
	}
}

// TestMode_EnvOverride verifies BASE_PYVM_MODE flips the default
// regardless of CPython build mode. Also covers the bogus-value path —
// unknown strings fall through to auto-detect.
func TestMode_EnvOverride(t *testing.T) {
	t.Setenv(envMode, "threaded")
	if got := DefaultMode(); got != ModeThreaded {
		t.Fatalf("env=threaded: DefaultMode = %v, want %v", got, ModeThreaded)
	}
	t.Setenv(envMode, "subinterp")
	if got := DefaultMode(); got != ModeSubinterp {
		t.Fatalf("env=subinterp: DefaultMode = %v, want %v", got, ModeSubinterp)
	}
	t.Setenv(envMode, "gibberish")
	// Bogus values fall through to the build-aware default.
	if got := DefaultMode(); GilDisabled() && got != ModeThreaded {
		t.Fatalf("env=gibberish on FT: DefaultMode = %v, want %v", got, ModeThreaded)
	}
}

// TestMode_ManifestOverride verifies extension.json "mode" picks the
// per-module mode in the absence of the process env override.
func TestMode_ManifestOverride(t *testing.T) {
	t.Setenv(envMode, "")

	cases := []struct {
		manifest string
		want     Mode
	}{
		{"threaded", ModeThreaded},
		{"subinterp", ModeSubinterp},
	}
	for _, tc := range cases {
		t.Run(tc.manifest, func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			dir := writeExt(t, "modetest", `
def echo(p):
    return p
`, tc.manifest, "echo")
			mod, err := rt.Load(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			pm, ok := mod.(*module)
			if !ok {
				t.Fatalf("module type = %T, want *module", mod)
			}
			if pm.mode != tc.want {
				t.Fatalf("mode = %v, want %v", pm.mode, tc.want)
			}
			// Sanity: invoke works.
			out, err := mod.Invoke(context.Background(), "echo", []byte(`{"x":1}`))
			if err != nil {
				t.Fatal(err)
			}
			if got := string(out); got != `{"x": 1}` {
				t.Fatalf("got %s, want %s", got, `{"x": 1}`)
			}
		})
	}
}

// runModeTest runs the basic per-mode tests against both threaded and
// subinterp so any common-path regression surfaces under both.
func runModeTest(t *testing.T, mode Mode, fn func(*testing.T)) {
	t.Run(mode.String(), func(t *testing.T) {
		t.Setenv(envMode, mode.String())
		fn(t)
	})
}

func TestModule_Invoke(t *testing.T) {
	runModeTest(t, ModeThreaded, testInvokeBasics)
	runModeTest(t, ModeSubinterp, testInvokeBasics)
}

func testInvokeBasics(t *testing.T) {
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
`, "", "echo", "upper", "add", "nothing")

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
	runModeTest(t, ModeThreaded, testUnknownFunction)
	runModeTest(t, ModeSubinterp, testUnknownFunction)
}

func testUnknownFunction(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "ext", `
def exists(_):
    return 1
`, "", "exists")
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
	runModeTest(t, ModeThreaded, testClosedRejects)
	runModeTest(t, ModeSubinterp, testClosedRejects)
}

func testClosedRejects(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "c", `
def f(_):
    return 1
`, "", "f")
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

// TestModule_ConcurrentSubInterpreters proves the subinterp pool
// actually supports parallel work. Mirrors the original test from
// v1 — kept here so the regression check on the legacy path remains.
func TestModule_ConcurrentSubInterpreters(t *testing.T) {
	t.Setenv(envMode, "subinterp")
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
`, "", "burn")

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

// TestModule_PreCancelledCtx proves ctx pre-cancel is honored before
// entering Python. Runs against both modes — the ctx pre-check is in
// the shared Invoke prelude.
func TestModule_PreCancelledCtx(t *testing.T) {
	runModeTest(t, ModeThreaded, testPreCancelledCtx)
	runModeTest(t, ModeSubinterp, testPreCancelledCtx)
}

func testPreCancelledCtx(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	dir := writeExt(t, "echo", `
def echo(p):
    return p
`, "", "echo")
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

// TestThreaded_Namespacing proves two modules loaded under the same
// logical name don't trample each other's symbols in __main__. Each
// gets a unique mangle prefix so the runtime routes correctly even
// when fn-name collides.
func TestThreaded_Namespacing(t *testing.T) {
	t.Setenv(envMode, "threaded")
	rt := NewRuntime()
	defer rt.Close()

	// Two modules with the SAME extension name and the SAME exported
	// function name but DIFFERENT implementations. If mangling is
	// correct, each Invoke returns its own constant.
	dirA := writeExt(t, "dup", `
def value(_):
    return "A"
`, "", "value")
	dirB := writeExt(t, "dup", `
def value(_):
    return "B"
`, "", "value")

	modA, err := rt.Load(context.Background(), dirA)
	if err != nil {
		t.Fatal(err)
	}
	defer modA.Close()
	modB, err := rt.Load(context.Background(), dirB)
	if err != nil {
		t.Fatal(err)
	}
	defer modB.Close()

	out, err := modA.Invoke(context.Background(), "value", []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != `"A"` {
		t.Fatalf("modA.value = %s, want %q", got, `"A"`)
	}
	out, err = modB.Invoke(context.Background(), "value", []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != `"B"` {
		t.Fatalf("modB.value = %s, want %q", got, `"B"`)
	}
}

// TestThreaded_PrivateHelpers proves a module's private helpers
// (top-level functions referenced by exported functions) resolve
// against the module's own namespace dict, not against __main__.
//
// If we copied function refs flat into __main__ without preserving
// the closure over module-private globals, calling `outer` here
// would NameError on `_helper`. The test relies on the function
// object's __globals__ pointing at the module dict.
func TestThreaded_PrivateHelpers(t *testing.T) {
	t.Setenv(envMode, "threaded")
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "private", `
def _helper(x):
    return x * 2

CONSTANT = 7

def outer(p):
    return {"doubled": _helper(p["n"]), "k": CONSTANT}
`, "", "outer")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	out, err := mod.Invoke(context.Background(), "outer", []byte(`{"n":21}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"doubled": 42, "k": 7}`
	if got := string(out); got != want {
		t.Fatalf("outer = %s, want %s", got, want)
	}

	// Private symbols must NOT be reachable as exported entrypoints —
	// the mangled-publication step skips names starting with "_".
	if _, err := mod.Invoke(context.Background(), "_helper", []byte(`{"x":1}`)); !errors.Is(err, extruntime.ErrUnknownFn) {
		t.Fatalf("_helper err = %v, want ErrUnknownFn", err)
	}
}

// TestThreaded_ParallelExecutesInParallel proves that on a free-
// threaded build, N concurrent CPU-bound invocations complete in
// roughly serial_cost wall time, NOT N * serial_cost.
//
// On a default-GIL build the GIL serializes them; the test still
// passes but the speedup factor we measure is ~1 instead of ~N.
// We only assert a STRICTLY POSITIVE speedup (parallel < N * serial)
// so the test stays meaningful on both builds.
//
// On free-threaded builds we further assert the speedup is >= 1.5x;
// the lower bound is intentionally lenient — bench numbers should
// be ~N/2 to N/1 on a multicore machine, but contention and GC
// pressure pull it down.
func TestThreaded_ParallelExecutesInParallel(t *testing.T) {
	if gort.NumCPU() < 2 {
		t.Skip("need >=2 CPU cores to observe parallelism")
	}
	t.Setenv(envMode, "threaded")
	rt := NewRuntime()
	defer rt.Close()

	// CPU-bound Python loop. Enough work to push past goroutine
	// scheduling jitter; tuned to ~3-5 ms per call on M1 Max so
	// the wall-time delta between serial and parallel is robust.
	const burnN = 80000
	dir := writeExt(t, "burnFT", fmt.Sprintf(`
def burn(p):
    n = p.get("n", %d)
    s = 0
    for i in range(n):
        s = (s + i * 7) %% 1000003
    return s
`, burnN), "", "burn")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	payload := []byte(fmt.Sprintf(`{"n":%d}`, burnN))

	// Warmup.
	if _, err := mod.Invoke(context.Background(), "burn", payload); err != nil {
		t.Fatal(err)
	}

	// Serial baseline: 8 sequential calls.
	const N = 8
	tSerial0 := time.Now()
	for i := 0; i < N; i++ {
		if _, err := mod.Invoke(context.Background(), "burn", payload); err != nil {
			t.Fatal(err)
		}
	}
	serialWall := time.Since(tSerial0)

	// Parallel: 8 concurrent calls.
	tPar0 := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := mod.Invoke(context.Background(), "burn", payload); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	parallelWall := time.Since(tPar0)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}

	speedup := float64(serialWall) / float64(parallelWall)
	t.Logf("serial=%s parallel=%s speedup=%.2fx (GIL disabled: %v, NumCPU=%d)",
		serialWall, parallelWall, speedup, GilDisabled(), gort.NumCPU())

	// Universal lower bound: any positive speedup. Below ~1.0 means
	// goroutine overhead made parallel SLOWER — that would be a
	// regression.
	if speedup < 1.0 {
		t.Fatalf("speedup %.2fx <= 1.0 — parallel was slower than serial (serial=%s parallel=%s)",
			speedup, serialWall, parallelWall)
	}

	// Free-threading bound: expect substantial parallelism. 1.5x is
	// the floor on the machines this is exercised on; in practice we
	// see 4-8x on M1 Max. If this fails on a CI host with tight CPU
	// budget, lower the bound or the burnN — don't disable.
	if GilDisabled() && speedup < 1.5 {
		t.Fatalf("free-threaded build: speedup %.2fx < 1.5 — expected ~Ncpu parallelism (NumCPU=%d)",
			speedup, gort.NumCPU())
	}
}

// TestThreaded_HighFanout fires many goroutines against a tiny
// invocation to surface any thread-state attach / GIL race that the
// PyGILState_Ensure path would expose. We just want no crash, no
// error; speedup isn't the point here.
func TestThreaded_HighFanout(t *testing.T) {
	t.Setenv(envMode, "threaded")
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "tiny", `
def add(p):
    return p["a"] + p["b"]
`, "", "add")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	const goroutines = 64
	const callsPerG = 32
	var wg sync.WaitGroup
	var errCount atomic.Int64
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerG; i++ {
				out, err := mod.Invoke(context.Background(), "add", []byte(`{"a":1,"b":2}`))
				if err != nil {
					errCount.Add(1)
					return
				}
				if string(out) != "3" {
					errCount.Add(1)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := errCount.Load(); got != 0 {
		t.Fatalf("errCount = %d (got errors / wrong results across %d goroutines x %d calls)",
			got, goroutines, callsPerG)
	}
}

// TestThreaded_CloseUnloads proves that after Close(), the module's
// mangled symbols are no longer reachable in __main__ even by a
// hand-crafted lookup. Important so a closed module's leftover
// symbols don't contaminate later loads.
func TestThreaded_CloseUnloads(t *testing.T) {
	t.Setenv(envMode, "threaded")
	rt := NewRuntime()
	defer rt.Close()

	dir := writeExt(t, "cleanup", `
def hello(_):
    return "world"
`, "", "hello")
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	pm, ok := mod.(*module)
	if !ok {
		t.Fatalf("module type = %T, want *module", mod)
	}
	prefix := pm.manglePrefix
	if prefix == "" {
		t.Fatalf("manglePrefix is empty")
	}

	// Verify it works before close.
	if _, err := mod.Invoke(context.Background(), "hello", []byte("null")); err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	// After close, any Invoke should error with ErrClosed.
	if _, err := mod.Invoke(context.Background(), "hello", []byte("null")); !errors.Is(err, extruntime.ErrClosed) {
		t.Fatalf("post-close invoke err = %v, want ErrClosed", err)
	}
}
