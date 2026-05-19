package gojavm

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
// returns the directory path. Helpers like this make every test below
// a one-liner — write the JS, point at the dir, invoke.
func writeExt(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := extruntime.Manifest{
		Name:    name,
		Version: "0.0.1",
		Runtime: "goja",
		Module:  "index.js",
		Exports: []string{"fn"},
	}
	mb, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), mb, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(src), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	return dir
}

func TestRuntime_NameAndCaps(t *testing.T) {
	r := NewRuntime()
	defer r.Close()

	if r.Name() != "goja" {
		t.Fatalf("Name = %q, want goja", r.Name())
	}
	caps := r.Capabilities()
	if len(caps.AcceptsLanguages) != 1 || caps.AcceptsLanguages[0] != "js" {
		t.Fatalf("AcceptsLanguages = %v, want [js]", caps.AcceptsLanguages)
	}
	if caps.HardSandbox {
		t.Fatal("HardSandbox should be false for goja")
	}
	if caps.Cgo {
		t.Fatal("Cgo should be false for goja")
	}
	if !caps.SupportsAbort {
		t.Fatal("SupportsAbort should be true (Interrupt is cooperative but present)")
	}
}

func TestRuntime_LoadRejectsWrongRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extension.json"),
		[]byte(`{"name":"x","runtime":"wazero","module":"x.wasm"}`), 0644); err != nil {
		t.Fatal(err)
	}
	r := NewRuntime()
	defer r.Close()
	if _, err := r.Load(context.Background(), dir); err == nil {
		t.Fatal("expected error loading wazero manifest in goja runtime")
	}
}

func TestModule_InvokeRoundtrip(t *testing.T) {
	dir := writeExt(t, "echo", `globalThis.fn = function(p) { return { got: p, doubled: p.n * 2 }; };`)

	r := NewRuntime()
	defer r.Close()

	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	if m.Name() != "echo" || m.Runtime() != "goja" {
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
	if got.Got["n"].(float64) != 21 {
		t.Fatalf("got.n = %v, want 21", got.Got["n"])
	}
}

func TestModule_InvokeUnknownFn(t *testing.T) {
	dir := writeExt(t, "u", `globalThis.fn = function() { return 1; };`)
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
	dir := writeExt(t, "c", `globalThis.fn = function() { return 1; };`)
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
	// A while(true) loop that calls a host-visible function each
	// iteration — goja's interrupt only fires at function-call
	// opcodes so we keep one in the loop body.
	src := `globalThis.fn = function() {
		var x = 0;
		while (true) { x = (x + 1) | 0; Math.sin(x); }
		return x;
	};`
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
		t.Fatalf("interrupt took too long: %v", elapsed)
	}
}

func TestModule_PrecompiledOnceAcrossInvocations(t *testing.T) {
	// If we re-run the script on every Invoke, top-level state mutates
	// each time. A counter on globalThis is the canary.
	src := `
		if (globalThis.__count === undefined) globalThis.__count = 0;
		globalThis.__count++;
		globalThis.fn = function() { return { c: globalThis.__count }; };
	`
	dir := writeExt(t, "once", src)

	r := NewRuntime()
	defer r.Close()
	m, err := r.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	// Force the same pool slot every time by serializing — pool runs
	// sequentially so the first non-busy slot is reused. We expect the
	// counter to stay at 1 across many invocations on the same slot.
	for i := 0; i < 5; i++ {
		out, err := m.Invoke(context.Background(), "fn", []byte(`{}`))
		if err != nil {
			t.Fatalf("Invoke[%d]: %v", i, err)
		}
		// All we care about is "the counter didn't grow with each
		// invocation on the same runtime". We don't assert ==1 since
		// the pool may hand us different slots, but it must never
		// exceed the pool size.
		var got struct {
			C int `json:"c"`
		}
		_ = json.Unmarshal(out, &got)
		if got.C > defaultPoolSize {
			t.Fatalf("counter %d exceeds pool size — script ran more than once per slot", got.C)
		}
	}
}
