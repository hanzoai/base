package extbench

import (
	"context"
	"testing"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/gojavm"
	"github.com/hanzoai/base/plugins/pyvm"
	"github.com/hanzoai/base/plugins/starkvm"
	"github.com/hanzoai/base/plugins/v8vm"
	"github.com/hanzoai/base/plugins/wasmvm"
)

// Coldstart benchmarks measure the cost of one Load() — fresh runtime,
// fresh module, no warmup. Each iteration constructs a new runtime,
// loads the fixture, then closes both. ns/op is the per-load cost.
//
// Cold start dominates short-lived deploys (lambdas, ephemeral hooks).
// For long-lived workers, Invoke cost matters more — see bench_test.go.

func benchColdstart(b *testing.B, factory func() extruntime.Runtime, fixture string) {
	b.Helper()
	if fixtureDir(fixture) == "" {
		b.Skipf("fixture %s missing", fixture)
	}
	// Probe load once to confirm artifact is present (e.g. validate.wasm
	// not yet built). Skip cleanly otherwise.
	probe := factory()
	dir := fixtureDir(fixture)
	if _, err := probe.Load(context.Background(), dir); err != nil {
		_ = probe.Close()
		b.Skipf("coldstart %s: probe load failed (%v)", fixture, err)
	}
	_ = probe.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt := factory()
		mod, err := rt.Load(context.Background(), dir)
		if err != nil {
			b.Fatalf("load: %v", err)
		}
		_ = mod.Close()
		_ = rt.Close()
	}
}

func BenchmarkColdstart_Native(b *testing.B) {
	benchColdstart(b, extruntime.NewNative, "native-go")
}

func BenchmarkColdstart_Goja(b *testing.B) {
	benchColdstart(b, gojavm.NewRuntime, "goja-js")
}

func BenchmarkColdstart_Starkvm(b *testing.B) {
	benchColdstart(b, starkvm.NewRuntime, "starkvm-star")
}

func BenchmarkColdstart_Wazero_AS(b *testing.B) {
	benchColdstart(b, wasmvm.NewRuntime, "wazero-as")
}

func BenchmarkColdstart_Wazero_Rust(b *testing.B) {
	benchColdstart(b, wasmvm.NewRuntime, "wazero-rust")
}

func BenchmarkColdstart_Wazero_Javy(b *testing.B) {
	benchColdstart(b, wasmvm.NewRuntime, "wazero-javy")
}

func BenchmarkColdstart_Wazero_CPythonWASI(b *testing.B) {
	benchColdstart(b, wasmvm.NewRuntime, "wazero-cpython-wasi")
}

func BenchmarkColdstart_Wazero_RustPython(b *testing.B) {
	benchColdstart(b, wasmvm.NewRuntime, "wazero-rustpython")
}

func BenchmarkColdstart_V8go(b *testing.B) {
	if !v8Available() {
		b.Skip("v8go not built (use -tags v8vm)")
	}
	benchColdstart(b, v8vm.NewRuntime, "v8go-js")
}

func BenchmarkColdstart_Pyvm(b *testing.B) {
	if !pyAvailable() {
		b.Skip("pyvm not built (use -tags pyvm)")
	}
	benchColdstart(b, pyvm.NewRuntime, "pyvm-py")
}
