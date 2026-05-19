//go:build pyvm
// +build pyvm

// Package pyvm is the CPython (cgo) extension runtime for Base.
//
// Two execution modes coexist behind a single Runtime interface:
//
//	ModeThreaded   — one global CPython interpreter, N goroutines pinned
//	                 to N OS threads invoke functions in parallel. On a
//	                 free-threaded (PEP 703 / python3.13t) build this is
//	                 actual parallel Python; on default-GIL the GIL
//	                 serializes them. This is the default.
//
//	ModeSubinterp  — one PEP 684 OWN_GIL sub-interpreter pool per loaded
//	                 module, each interpreter holding its own GIL. Strong
//	                 isolation between sub-interpreters; bounded
//	                 parallelism by pool size. Opt-in via extension.json
//	                 "mode": "subinterp" OR BASE_PYVM_MODE=subinterp.
//
// Mode selection precedence: extension.json field > env var override >
// auto-detect from CPython build (threaded on free-threaded builds,
// subinterp otherwise).
//
// Both modes share the same Invoke contract: LockOSThread → enter GIL
// → call __main__.fn(json.loads(payload)) → json.dumps → leave GIL →
// UnlockOSThread. They differ in what "enter GIL" means (attach vs
// swap thread state) and in symbol resolution (mangled prefix vs
// per-sub-interp __main__).
//
// Free-threading detection
// ------------------------
// pyvm_gil_disabled() returns 1 if libpython was compiled with
// Py_GIL_DISABLED (--disable-gil), 0 otherwise. The C bridge sets this
// at static init time so Go can branch on it cheaply.
//
// Crash isolation caveat
// ----------------------
// A C extension segfault crashes the whole Go process. HIP-0105 marks
// pyvm as single-tenant deploys only; multi-tenant production should
// use wazero + a Python compiled to wasm (RustPython / pyodide / cpython
// wasm32-wasi). See plugins/pyvm/README.md.
package pyvm

/*
#cgo pkg-config: python-3.13-embed
#include "pyvm_bridge.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"unsafe"

	"github.com/hanzoai/base/plugins/extruntime"
)

// Mode picks the per-module execution strategy. ModeThreaded is the
// default on free-threaded CPython builds; ModeSubinterp is the
// default on default-GIL builds (and the only mode prior to v2).
type Mode int

const (
	// ModeThreaded runs every Invoke against the single global
	// interpreter. Goroutines pin to OS threads and call
	// PyGILState_Ensure to attach. Cheap, parallel-on-FT, but no
	// per-tenant memory isolation: tenants share __main__.
	ModeThreaded Mode = iota

	// ModeSubinterp runs every Invoke against a per-module pool of
	// PEP 684 OWN_GIL sub-interpreters. Each interpreter holds its
	// own __main__, modules, and GIL. Strong isolation but pool-
	// size bounded parallelism and high per-module memory cost.
	ModeSubinterp
)

func (m Mode) String() string {
	switch m {
	case ModeThreaded:
		return "threaded"
	case ModeSubinterp:
		return "subinterp"
	default:
		return fmt.Sprintf("mode(%d)", int(m))
	}
}

// parseMode parses extension.json "mode" or env var values. Unknown
// values map to ModeThreaded by default (forward-compatible default).
func parseMode(s string) (Mode, bool) {
	switch s {
	case "threaded":
		return ModeThreaded, true
	case "subinterp":
		return ModeSubinterp, true
	default:
		return ModeThreaded, false
	}
}

// Defaults — tuned for short-lived per-request Python hooks.
const (
	defaultPoolSize = 4
	envPoolSize     = "BASE_PYVM_POOL_SIZE"
	envMode         = "BASE_PYVM_MODE"
)

var (
	gInit    sync.Once
	gInitErr error
	gGilOff  bool
)

func initPython() error {
	gInit.Do(func() {
		C.pyvm_init()
		if C.pyvm_gil_disabled() != 0 {
			gGilOff = true
		}
	})
	return gInitErr
}

// NewRuntime returns a pyvm-backed Runtime. CPython is initialized on
// first use and shared across every module loaded by this runtime.
// Mode defaults to threaded on free-threaded builds, subinterp on
// default-GIL builds. Override per-module via extension.json or
// process-wide via BASE_PYVM_MODE.
func NewRuntime() extruntime.Runtime {
	r := &runtime{
		poolSize: envInt(envPoolSize, defaultPoolSize),
	}
	return r
}

type runtime struct {
	poolSize int

	mu     sync.Mutex
	closed bool
}

func (*runtime) Name() string { return "pyvm" }

func (*runtime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"py"},
		HardSandbox:      false,
		Cgo:              true,
		// HONEST: we can't hard-abort a running invocation. ctx pre-
		// cancel works; ctx cancel mid-Python does not. See README.
		SupportsAbort: false,
	}
}

// GilDisabled reports whether the linked CPython was built with the
// free-threading (--disable-gil) flag enabled. Result is stable for
// the process lifetime after the first initPython call.
func GilDisabled() bool {
	_ = initPython()
	return gGilOff
}

// DefaultMode returns the mode that will be used when neither the
// extension.json nor BASE_PYVM_MODE specifies one. Exposed for tests
// and diagnostics; production code does not need to call this.
func DefaultMode() Mode {
	if v := os.Getenv(envMode); v != "" {
		if m, ok := parseMode(v); ok {
			return m
		}
	}
	_ = initPython()
	if gGilOff {
		return ModeThreaded
	}
	// On default-GIL builds, threaded mode runs but the GIL
	// serializes everything — subinterp at least delivers OWN_GIL
	// parallelism. Pick subinterp as the safer default.
	return ModeSubinterp
}

// resolveMode picks the mode for a given manifest, taking into account
// (in order): the manifest's own "mode" field, the BASE_PYVM_MODE
// process-wide override, and the runtime auto-detection.
//
// Forwarding override semantics: BASE_PYVM_MODE overrides ANY manifest.
// This matches the existing BASE_PYVM_POOL_SIZE pattern — operations
// owners have a single env knob to flip the whole process.
func resolveMode(manifestMode string) Mode {
	if v := os.Getenv(envMode); v != "" {
		if m, ok := parseMode(v); ok {
			return m
		}
	}
	if manifestMode != "" {
		if m, ok := parseMode(manifestMode); ok {
			return m
		}
	}
	return DefaultMode()
}

func (r *runtime) Load(_ context.Context, dir string) (extruntime.Module, error) {
	if err := initPython(); err != nil {
		return nil, fmt.Errorf("pyvm: init: %w", err)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	r.mu.Unlock()

	m, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Runtime != "pyvm" {
		return nil, fmt.Errorf("%w: pyvm runtime cannot load %q runtime", extruntime.ErrUnsupported, m.Runtime)
	}
	if m.Module == "" {
		return nil, fmt.Errorf("%w: module path is required", extruntime.ErrBadManifest)
	}

	src, err := os.ReadFile(joinPath(dir, m.Module))
	if err != nil {
		return nil, fmt.Errorf("read module %s: %w", m.Module, err)
	}

	mode := resolveMode(extensionMode(m))
	return newModule(r, m, string(src), mode)
}

// extensionMode pulls the "mode" field out of the manifest. The
// extruntime.Manifest struct doesn't define this field today because
// it's pyvm-specific — we round-trip via the raw JSON to read it.
func extensionMode(m *extruntime.Manifest) string {
	// The Manifest struct ignores unknown fields, so we re-read the
	// file. Lookup is cheap (file is already in OS cache) and avoids
	// adding a pyvm-specific field to the shared Manifest type.
	return m.Mode()
}

func (r *runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	// We do NOT call Py_FinalizeEx here. CPython finalize/re-init is
	// fragile, especially with sub-interpreters held by other modules.
	// Process exit reclaims the runtime — this matches v8go's pattern
	// of leaving the engine alive until the process dies.
	return nil
}

func joinPath(dir, mod string) string {
	if dir == "" {
		return mod
	}
	if len(dir) > 0 && dir[len(dir)-1] == os.PathSeparator {
		return dir + mod
	}
	return dir + string(os.PathSeparator) + mod
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// cString allocates a C string. Caller must C.free.
func cString(s string) *C.char { return C.CString(s) }

// goString copies a C string and frees the original.
func goStringAndFree(c *C.char) string {
	if c == nil {
		return ""
	}
	s := C.GoString(c)
	C.free(unsafe.Pointer(c))
	return s
}

// goErrorAndFree returns a Go error built from a C error string and
// frees it. Returns nil if c is nil.
func goErrorAndFree(c *C.char, prefix string) error {
	if c == nil {
		return nil
	}
	s := C.GoString(c)
	C.free(unsafe.Pointer(c))
	return errors.New(prefix + ": " + s)
}
