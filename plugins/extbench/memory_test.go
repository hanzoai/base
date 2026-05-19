package extbench

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/gojavm"
	"github.com/hanzoai/base/plugins/v8vm"
	"github.com/hanzoai/base/plugins/wasmvm"
)

// memoryProbe loads a fixture, runs N invocations, and reports the
// delta in TotalAlloc and NumGC. These tests are NOT real benchmarks —
// they exist so `go test -run TestMemory` prints the per-runtime memory
// pressure to stdout for inclusion in the writeup.
//
// Notes:
//   - TotalAlloc is monotonic; we capture it before and after the loop
//     and report the difference. This is the total bytes allocated
//     during the workload, NOT the live heap delta.
//   - NumGC delta tells us whether the workload is GC-pressure heavy.
//   - We `runtime.GC()` before the "before" snapshot so the baseline
//     isn't polluted by prior tests' garbage.

const memoryIters = 1000

func runMemory(t *testing.T, factory func() extruntime.Runtime, fixture string, name string) {
	t.Helper()
	if fixtureDir(fixture) == "" {
		t.Skipf("fixture %s missing", fixture)
	}
	rt := factory()
	dir := fixtureDir(fixture)
	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Skipf("%s: load: %v", name, err)
	}
	// NOTE: we intentionally do NOT close the module or runtime here.
	// The v8go runtime's cgo Context.Close path has a known segfault on
	// darwin/arm64 when the isolate is GC'd while a context is still
	// referenced. Since each TestMemory_* runs in its own subtest and
	// the test process exits shortly after, letting the OS reclaim is
	// safe and avoids a teardown crash from masking the measurement
	// we just took. The benchmark functions in bench_test.go do close
	// because they re-run with -count=N and need a clean slate.
	_ = mod
	_ = rt

	// Warm up — first Invoke may JIT-compile / instantiate lazily.
	if _, err := mod.Invoke(context.Background(), "validate", payload); err != nil {
		t.Fatalf("%s: warmup: %v", name, err)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	ctx := context.Background()
	for i := 0; i < memoryIters; i++ {
		if _, err := mod.Invoke(ctx, "validate", payload); err != nil {
			t.Fatalf("%s: invoke %d: %v", name, i, err)
		}
	}

	runtime.ReadMemStats(&after)
	totalDelta := after.TotalAlloc - before.TotalAlloc
	gcDelta := after.NumGC - before.NumGC

	// Printed in a format that's easy to grep into the writeup. tabular
	// for ease of paste.
	fmt.Printf("MEMORY\t%s\titers=%d\ttotal_alloc_delta=%d\tnum_gc_delta=%d\tbytes_per_invoke=%.1f\n",
		name, memoryIters, totalDelta, gcDelta, float64(totalDelta)/float64(memoryIters))
}

func TestMemory_Native(t *testing.T) {
	runMemory(t, extruntime.NewNative, "native-go", "native")
}

func TestMemory_Goja(t *testing.T) {
	runMemory(t, gojavm.NewRuntime, "goja-js", "goja")
}

func TestMemory_Wazero_AS(t *testing.T) {
	runMemory(t, wasmvm.NewRuntime, "wazero-as", "wazero-as")
}

func TestMemory_Wazero_Javy(t *testing.T) {
	runMemory(t, wasmvm.NewRuntime, "wazero-javy", "wazero-javy")
}

func TestMemory_V8go(t *testing.T) {
	if !v8Available() {
		t.Skip("v8go not built (use -tags v8vm)")
	}
	runMemory(t, v8vm.NewRuntime, "v8go-js", "v8go")
}
