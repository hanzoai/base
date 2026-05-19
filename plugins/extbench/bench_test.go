// Package extbench benchmarks the five Base extension runtimes against
// the same workload: validate(email, age) -> normalize + shape check.
//
// One fixture per runtime under fixtures/<runtime>/. Each fixture
// implements identical semantics:
//
//	in:  {"email":"Foo@Example.COM ","age":25}
//	ok:  {"ok":true,...,"age":25}
//	err: {"ok":false,"error":...}
//
// Per-runtime ns/op compares apples-to-apples because the workload is
// identical bytes in, identical bytes out (modulo per-runtime field
// ordering which doesn't affect cost).
package extbench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/gojavm"
	"github.com/hanzoai/base/plugins/pyvm"
	"github.com/hanzoai/base/plugins/v8vm"
	"github.com/hanzoai/base/plugins/wasmvm"

	// Side-effect import: registers validate-email:validate with the
	// native runtime registry.
	_ "github.com/hanzoai/base/plugins/extbench/fixtures/native-go"
)

// payload is the fixed input for every benchmark. We pre-marshal once
// and pass the same byte slice into every Invoke so per-call cost is
// strictly the runtime's overhead, not JSON encoding on the host.
var payload = []byte(`{"email":"Foo@Example.COM ","age":25}`)

// fixtureDir resolves the directory for a named fixture. Returns "" if
// the fixture is unavailable (e.g. compiled artifact missing) so the
// caller can b.Skip cleanly.
func fixtureDir(name string) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Join(filepath.Dir(thisFile), "fixtures", name)
	if _, err := os.Stat(filepath.Join(dir, "extension.json")); err != nil {
		return ""
	}
	return dir
}

// loadFixture loads a module from a fixture directory; the returned
// teardown must be deferred by the caller.
func loadFixture(b *testing.B, rt extruntime.Runtime, fixture string) (extruntime.Module, func()) {
	b.Helper()
	dir := fixtureDir(fixture)
	if dir == "" {
		b.Skipf("fixture %s missing", fixture)
	}
	// If the fixture references a compiled module that doesn't exist
	// yet (e.g. validate.wasm not built), surface as Skip rather than
	// fail. Compilation is a deploy-time concern.
	man, err := extruntime.LoadManifest(dir)
	if err != nil {
		b.Skipf("fixture %s: %v", fixture, err)
	}
	if man.Module != "" {
		if _, err := os.Stat(filepath.Join(dir, man.Module)); err != nil {
			b.Skipf("fixture %s: module %s not built (%v)", fixture, man.Module, err)
		}
	}
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		b.Fatalf("load %s: %v", fixture, err)
	}
	return mod, func() {
		_ = mod.Close()
		_ = rt.Close()
	}
}

// sanityCheck runs one Invoke outside the hot loop. We don't validate
// the bytes equal a known string (field order varies across runtimes);
// we only assert it didn't error and returned non-empty.
func sanityCheck(b *testing.B, mod extruntime.Module) {
	b.Helper()
	out, err := mod.Invoke(context.Background(), "validate", payload)
	if err != nil {
		b.Fatalf("sanity invoke: %v", err)
	}
	if len(out) == 0 {
		b.Fatalf("sanity invoke: empty result")
	}
}

// runSerial / runParallel are the per-benchmark hot loops. Hot loop
// stays tiny: Invoke + discard. Errors panic — the sanity check above
// already proved happy-path so an in-loop error is a bench bug.
func runSerial(b *testing.B, mod extruntime.Module) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mod.Invoke(ctx, "validate", payload); err != nil {
			b.Fatalf("invoke: %v", err)
		}
	}
}

func runParallel(b *testing.B, mod extruntime.Module) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			if _, err := mod.Invoke(ctx, "validate", payload); err != nil {
				b.Fatalf("invoke: %v", err)
			}
		}
	})
}

// ---------- native ----------

func BenchmarkNative_Serial(b *testing.B) {
	b.StopTimer()
	rt := extruntime.NewNative()
	mod, teardown := loadFixture(b, rt, "native-go")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runSerial(b, mod)
}

func BenchmarkNative_Parallel(b *testing.B) {
	b.StopTimer()
	rt := extruntime.NewNative()
	mod, teardown := loadFixture(b, rt, "native-go")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runParallel(b, mod)
}

// ---------- goja ----------

func BenchmarkGoja_Serial(b *testing.B) {
	b.StopTimer()
	rt := gojavm.NewRuntime()
	mod, teardown := loadFixture(b, rt, "goja-js")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runSerial(b, mod)
}

func BenchmarkGoja_Parallel(b *testing.B) {
	b.StopTimer()
	rt := gojavm.NewRuntime()
	mod, teardown := loadFixture(b, rt, "goja-js")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runParallel(b, mod)
}

// ---------- wazero (AssemblyScript) ----------

func BenchmarkWazero_AS_Serial(b *testing.B) {
	b.StopTimer()
	rt := wasmvm.NewRuntime()
	mod, teardown := loadFixture(b, rt, "wazero-as")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runSerial(b, mod)
}

func BenchmarkWazero_AS_Parallel(b *testing.B) {
	b.StopTimer()
	rt := wasmvm.NewRuntime()
	mod, teardown := loadFixture(b, rt, "wazero-as")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runParallel(b, mod)
}

// ---------- wazero (Javy / JS-on-wasm) ----------
//
// Javy emits a WASI command module (stdin/stdout JSON), which doesn't
// match Base's wasmvm pointer ABI. Until a shim lands, this benchmark
// skips with a clear message. See fixtures/wazero-javy/README.md.

func BenchmarkWazero_Javy_Serial(b *testing.B) {
	b.StopTimer()
	rt := wasmvm.NewRuntime()
	mod, teardown := loadFixture(b, rt, "wazero-javy")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runSerial(b, mod)
}

func BenchmarkWazero_Javy_Parallel(b *testing.B) {
	b.StopTimer()
	rt := wasmvm.NewRuntime()
	mod, teardown := loadFixture(b, rt, "wazero-javy")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runParallel(b, mod)
}

// ---------- v8go (build tag) ----------

func BenchmarkV8go_Serial(b *testing.B) {
	b.StopTimer()
	rt := v8vm.NewRuntime()
	if rt.Capabilities().Cgo && !v8Available() {
		b.Skip("v8go not built (use -tags v8vm)")
	}
	mod, teardown := loadFixture(b, rt, "v8go-js")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runSerial(b, mod)
}

func BenchmarkV8go_Parallel(b *testing.B) {
	b.StopTimer()
	rt := v8vm.NewRuntime()
	if rt.Capabilities().Cgo && !v8Available() {
		b.Skip("v8go not built (use -tags v8vm)")
	}
	mod, teardown := loadFixture(b, rt, "v8go-js")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runParallel(b, mod)
}

// v8Available probes whether the v8vm package was compiled with the
// real engine or the stub. The stub's Load always returns
// ErrUnsupported on a stub-only build. We do this by trying to Load
// the fixture and inspecting the error rather than introspecting the
// build tag.
func v8Available() bool {
	rt := v8vm.NewRuntime()
	defer rt.Close()
	dir := fixtureDir("v8go-js")
	if dir == "" {
		return false
	}
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		return false
	}
	_ = mod.Close()
	return true
}

// ---------- pyvm (CPython 3.13, build tag) ----------

func BenchmarkPyvm_Serial(b *testing.B) {
	b.StopTimer()
	rt := pyvm.NewRuntime()
	if rt.Capabilities().Cgo && !pyAvailable() {
		b.Skip("pyvm not built (use -tags pyvm)")
	}
	mod, teardown := loadFixture(b, rt, "pyvm-py")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runSerial(b, mod)
}

func BenchmarkPyvm_Parallel(b *testing.B) {
	b.StopTimer()
	rt := pyvm.NewRuntime()
	if rt.Capabilities().Cgo && !pyAvailable() {
		b.Skip("pyvm not built (use -tags pyvm)")
	}
	mod, teardown := loadFixture(b, rt, "pyvm-py")
	defer teardown()
	sanityCheck(b, mod)
	b.StartTimer()
	runParallel(b, mod)
}

// pyAvailable probes whether the pyvm package was compiled with the
// real engine or the stub.
func pyAvailable() bool {
	rt := pyvm.NewRuntime()
	defer rt.Close()
	dir := fixtureDir("pyvm-py")
	if dir == "" {
		return false
	}
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		return false
	}
	_ = mod.Close()
	return true
}
