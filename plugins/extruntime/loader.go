package extruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// Loader resolves extension manifests against a runtime registry and
// loads each extension with the runtime whose Name() matches the
// manifest's `runtime` field.
//
// Construct one Loader per process, register every runtime you have
// linked in, and call LoadDir() against your extensions directory.
// Failures on individual extensions are logged but do not abort the
// load — callers get back the modules that did load.
//
// Loader is safe to call from a single goroutine. Concurrent LoadDir
// calls are not supported (no real use case — boot once).
type Loader struct {
	runtimes map[string]Runtime
	log      *slog.Logger
}

// NewLoader builds a Loader keyed by each runtime's Name(). The last
// runtime registered for a given name wins; passing two runtimes with
// the same Name() is a caller bug.
func NewLoader(runtimes ...Runtime) *Loader {
	l := &Loader{
		runtimes: make(map[string]Runtime, len(runtimes)),
		log:      slog.Default(),
	}
	for _, r := range runtimes {
		if r == nil {
			continue
		}
		l.runtimes[r.Name()] = r
	}
	return l
}

// SetLogger swaps the logger used for per-extension load failures.
// nil restores slog.Default(). Returns the Loader for chaining.
func (l *Loader) SetLogger(log *slog.Logger) *Loader {
	if log == nil {
		log = slog.Default()
	}
	l.log = log
	return l
}

// Runtimes returns the registered runtime names, sorted. Useful for
// diagnostics and tests.
func (l *Loader) Runtimes() []string {
	names := make([]string, 0, len(l.runtimes))
	for n := range l.runtimes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// LoadDir scans dir for subdirectories containing an extension.json
// manifest and loads each one with the matching runtime. The returned
// map is keyed by manifest.Name (NOT by directory name) — two
// extensions with the same name in the same dir is a manifest bug and
// the second one logged-and-skipped.
//
// Missing dir is not an error: returns an empty map. Individual
// extension failures (bad manifest, unknown runtime, runtime.Load
// failure) are logged and skipped.
func (l *Loader) LoadDir(ctx context.Context, dir string) (map[string]Module, error) {
	out := map[string]Module{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("extruntime: scan %s: %w", dir, err)
	}

	// Deterministic order — predictable test output and predictable
	// load ordering for extensions that have init-time side effects.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		extDir := filepath.Join(dir, ent.Name())

		manifestPath := filepath.Join(extDir, "extension.json")
		if _, err := os.Stat(manifestPath); err != nil {
			// Not every subdir is an extension — silently skip
			// directories without a manifest.
			continue
		}

		mf, err := LoadManifest(extDir)
		if err != nil {
			l.log.Error("extruntime: load manifest failed", "dir", extDir, "err", err)
			continue
		}

		rt, ok := l.runtimes[mf.Runtime]
		if !ok {
			l.log.Error("extruntime: unknown runtime",
				"extension", mf.Name, "runtime", mf.Runtime, "dir", extDir)
			continue
		}

		mod, err := rt.Load(ctx, extDir)
		if err != nil {
			l.log.Error("extruntime: runtime load failed",
				"extension", mf.Name, "runtime", mf.Runtime, "dir", extDir, "err", err)
			continue
		}

		if existing, dup := out[mf.Name]; dup {
			l.log.Error("extruntime: duplicate extension name — keeping first, closing second",
				"name", mf.Name, "dir", extDir, "kept_runtime", existing.Runtime())
			_ = mod.Close()
			continue
		}
		out[mf.Name] = mod
	}

	return out, nil
}

// Close closes every registered runtime. It returns the first error
// encountered but tries every runtime regardless. Callers should Close
// individual modules before Close-ing the loader.
func (l *Loader) Close() error {
	var firstErr error
	for name, r := range l.runtimes {
		if err := r.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("extruntime: close %s: %w", name, err)
		}
	}
	return firstErr
}
