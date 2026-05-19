package extruntime_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/gojavm"
)

// fakeRuntime is a no-op runtime used to verify the loader's
// dispatch logic without pulling wazero/v8go into this test file —
// those runtimes have their own packages and tests; here we care
// only that the loader picks the right one.
type fakeRuntime struct {
	name     string
	loadErr  error
	loaded   []string
	capsLang []string
}

func (f *fakeRuntime) Name() string { return f.name }
func (f *fakeRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{AcceptsLanguages: f.capsLang}
}
func (f *fakeRuntime) Load(ctx context.Context, dir string) (extruntime.Module, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	m, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	f.loaded = append(f.loaded, m.Name)
	return &fakeModule{name: m.Name, runtime: f.name, exports: m.Exports}, nil
}
func (f *fakeRuntime) Close() error { return nil }

type fakeModule struct {
	name    string
	runtime string
	exports []string
}

func (m *fakeModule) Name() string      { return m.name }
func (m *fakeModule) Runtime() string   { return m.runtime }
func (m *fakeModule) Exports() []string { return m.exports }
func (m *fakeModule) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	return payload, nil
}
func (m *fakeModule) Close() error { return nil }

// writeManifest writes an extension.json + an optional module file into
// dir/<name>/. Returns the per-extension directory.
func writeManifest(t *testing.T, root, name, runtime, moduleFile, src string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	m := extruntime.Manifest{
		Name:    name,
		Version: "0.0.1",
		Runtime: runtime,
		Module:  moduleFile,
		Exports: []string{"fn"},
	}
	mb, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), mb, 0644); err != nil {
		t.Fatal(err)
	}
	if moduleFile != "" && src != "" {
		if err := os.WriteFile(filepath.Join(dir, moduleFile), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoader_RoutesToCorrectRuntime(t *testing.T) {
	root := t.TempDir()

	writeManifest(t, root, "native-ext", "native", "", "")
	writeManifest(t, root, "goja-ext", "goja", "index.js",
		`globalThis.fn = function(p) { return { ok: true }; };`)
	writeManifest(t, root, "wazero-ext", "wazero", "x.wasm", "")
	writeManifest(t, root, "v8go-ext", "v8go", "index.js", "")

	wazero := &fakeRuntime{name: "wazero", capsLang: []string{"wasm"}}
	v8 := &fakeRuntime{name: "v8go", capsLang: []string{"js"}}

	l := extruntime.NewLoader(
		extruntime.NewNative(),
		gojavm.NewRuntime(),
		wazero,
		v8,
	)
	defer l.Close()

	got := l.Runtimes()
	sort.Strings(got)
	want := []string{"goja", "native", "v8go", "wazero"}
	if len(got) != len(want) {
		t.Fatalf("Runtimes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Runtimes[%d] = %s, want %s", i, got[i], want[i])
		}
	}

	mods, err := l.LoadDir(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	for _, name := range []string{"native-ext", "goja-ext", "wazero-ext", "v8go-ext"} {
		m, ok := mods[name]
		if !ok {
			t.Errorf("missing module %s", name)
			continue
		}
		// Each module's Runtime() must match the manifest's runtime.
		var want string
		switch name {
		case "native-ext":
			want = "native"
		case "goja-ext":
			want = "goja"
		case "wazero-ext":
			want = "wazero"
		case "v8go-ext":
			want = "v8go"
		}
		if m.Runtime() != want {
			t.Errorf("module %s routed to runtime %s, want %s", name, m.Runtime(), want)
		}
		_ = m.Close()
	}
}

func TestLoader_UnknownRuntimeIsNonFatal(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "good-ext", "native", "", "")
	writeManifest(t, root, "bad-ext", "nonexistent-runtime", "", "")

	l := extruntime.NewLoader(extruntime.NewNative())
	defer l.Close()

	mods, err := l.LoadDir(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadDir should not fail on unknown runtime: %v", err)
	}
	if _, ok := mods["good-ext"]; !ok {
		t.Error("good-ext should have loaded")
	}
	if _, ok := mods["bad-ext"]; ok {
		t.Error("bad-ext should have been skipped")
	}
}

func TestLoader_BadManifestIsNonFatal(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "good-ext", "native", "", "")

	// Write a malformed manifest.
	bad := filepath.Join(root, "bad-ext")
	if err := os.MkdirAll(bad, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "extension.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	l := extruntime.NewLoader(extruntime.NewNative())
	defer l.Close()

	mods, err := l.LoadDir(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadDir should not fail on bad manifest: %v", err)
	}
	if _, ok := mods["good-ext"]; !ok {
		t.Error("good-ext should have loaded")
	}
	if len(mods) != 1 {
		t.Errorf("only good-ext should have loaded, got %d", len(mods))
	}
}

func TestLoader_MissingDir(t *testing.T) {
	l := extruntime.NewLoader(extruntime.NewNative())
	defer l.Close()

	mods, err := l.LoadDir(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadDir on missing dir should not error: %v", err)
	}
	if len(mods) != 0 {
		t.Errorf("expected empty map, got %d entries", len(mods))
	}
}

func TestLoader_SkipsNonExtensionDirs(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "real-ext", "native", "", "")

	// A subdir without extension.json — should be silently skipped.
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	l := extruntime.NewLoader(extruntime.NewNative())
	defer l.Close()

	mods, err := l.LoadDir(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(mods) != 1 {
		t.Errorf("want 1 module, got %d", len(mods))
	}
}

func TestLoader_RuntimeLoadErrorIsNonFatal(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "good-ext", "native", "", "")
	writeManifest(t, root, "failing-ext", "flaky", "", "")

	flaky := &fakeRuntime{name: "flaky", loadErr: os.ErrPermission}
	l := extruntime.NewLoader(extruntime.NewNative(), flaky)
	defer l.Close()

	mods, err := l.LoadDir(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if _, ok := mods["good-ext"]; !ok {
		t.Error("good-ext should have loaded")
	}
	if _, ok := mods["failing-ext"]; ok {
		t.Error("failing-ext should have been skipped after runtime load error")
	}
}
