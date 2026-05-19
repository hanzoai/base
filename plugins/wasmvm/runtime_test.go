package wasmvm_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/wasmvm"
)

// fixturesDir resolves the shared extbench fixtures path. Each test
// loads a manifest from there; tests that need a specific wasm binary
// skip themselves if the fixture isn't built.
//
// TODO(wasmvm): wire fixtures/validate/{extension.json,validate.wasm}
// into plugins/extbench/fixtures so these tests have real bytecode to
// exercise. Until then the table-driven tests skip rather than fail.
func fixturesDir(t *testing.T) string {
	t.Helper()
	// Resolve relative to this file's package — extbench sits next to
	// wasmvm under plugins/.
	root, err := filepath.Abs("../extbench/fixtures")
	if err != nil {
		t.Fatalf("resolve fixtures: %v", err)
	}
	return root
}

// fixtureOrSkip returns the directory containing the named fixture, or
// skips the test if the fixture (or its compiled wasm) isn't present.
func fixtureOrSkip(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(fixturesDir(t), name)
	manifest := filepath.Join(dir, "extension.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Skipf("fixture %s not built (%v); run extbench fixture build", name, err)
	}
	return dir
}

func TestRuntimeIdentity(t *testing.T) {
	rt := wasmvm.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })

	if rt.Name() != "wazero" {
		t.Errorf("Name() = %q, want %q", rt.Name(), "wazero")
	}
	caps := rt.Capabilities()
	if !caps.HardSandbox || !caps.SupportsAbort {
		t.Errorf("Capabilities() = %+v, want HardSandbox & SupportsAbort", caps)
	}
	if caps.Cgo {
		t.Errorf("wazero is pure-go, Cgo should be false")
	}
	if len(caps.AcceptsLanguages) != 1 || caps.AcceptsLanguages[0] != "wasm" {
		t.Errorf("AcceptsLanguages = %v, want [wasm]", caps.AcceptsLanguages)
	}
}

func TestLoadAndInvoke(t *testing.T) {
	dir := fixtureOrSkip(t, "validate")
	rt := wasmvm.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })

	ctx := context.Background()
	mod, err := rt.Load(ctx, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = mod.Close() })

	if mod.Runtime() != "wazero" {
		t.Errorf("module.Runtime() = %q, want wazero", mod.Runtime())
	}

	out, err := mod.Invoke(ctx, "validate", []byte(`{"email":"z@hanzo.ai"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(out) == 0 {
		t.Errorf("Invoke returned empty payload — fixture should echo a JSON object")
	}
}

func TestContextCancelAborts(t *testing.T) {
	dir := fixtureOrSkip(t, "spin")
	rt := wasmvm.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })

	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = mod.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = mod.Invoke(ctx, "spin", []byte("{}"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected cancellation error, got nil after %v", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx error, got %v", err)
	}
	// Hard abort should return well within a second; if it takes
	// >500ms the cancel-via-Close path isn't engaging.
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel took %v, expected hard abort under 500ms", elapsed)
	}
}

func TestPoolReuseConcurrent(t *testing.T) {
	dir := fixtureOrSkip(t, "validate")

	// Force pool size 4 for this test; 16 concurrent invocations
	// must serialize through that pool without deadlock.
	t.Setenv("BASE_WASMVM_POOL_SIZE", "4")

	rt := wasmvm.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })

	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = mod.Close() })

	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := mod.Invoke(context.Background(), "validate", []byte(`{"email":"z@hanzo.ai"}`))
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent Invoke: %v", e)
	}
}

func TestInvokeAfterClose(t *testing.T) {
	dir := fixtureOrSkip(t, "validate")
	rt := wasmvm.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })

	mod, err := rt.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mod.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = mod.Invoke(context.Background(), "validate", []byte("{}"))
	if !errors.Is(err, extruntime.ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestLoadRejectsNonWazeroManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"x","version":"0.0.0","runtime":"native","module":"x.wasm"}`
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := wasmvm.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })

	_, err := rt.Load(context.Background(), dir)
	if !errors.Is(err, extruntime.ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}
