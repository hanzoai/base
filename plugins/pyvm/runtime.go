//go:build pyvm
// +build pyvm

// Package pyvm is the CPython (cgo) extension runtime for Base.
//
// Design
// ------
// CPython is initialized once per process. Each loaded module owns a pool
// of sub-interpreters (PEP 684 OWN_GIL on 3.12+, legacy SHARED_GIL on
// older builds) so that two modules — and two concurrent invocations of
// the same module — can run without contending on the main interpreter.
//
// Each Invoke pins itself to one OS thread (runtime.LockOSThread),
// acquires a sub-interpreter from the module's pool, runs the function,
// returns the sub-interpreter to the pool, and unpins. Context cancel
// fires a Py_AddPendingCall watchdog that raises KeyboardInterrupt in
// the running interpreter — cooperative but reliable for any Python code
// that doesn't sit in a tight uninterruptible C call.
//
// Free-threading (PEP 703)
// ------------------------
// If CPython was built with --disable-gil (Py_GIL_DISABLED=1), the GIL
// is a no-op and OWN_GIL sub-interpreters become unnecessary for
// parallelism — every interpreter runs truly in parallel on its OS
// thread. The pool semantics remain the same; we simply skip the
// extra ceremony on free-threaded builds. Detection happens at
// runtime via Py_IsGILDisabled (3.13+) with a fallback path.
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

// Defaults — tuned for short-lived per-request Python hooks.
const (
	defaultPoolSize = 4
	envPoolSize     = "BASE_PYVM_POOL_SIZE"
)

var (
	gInit     sync.Once
	gInitErr  error
	gGilOff   bool
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
// first use and shared across every module loaded by this runtime;
// modules own their sub-interpreters. Multiple NewRuntime() calls in
// the same process all share the single CPython init.
func NewRuntime() extruntime.Runtime {
	r := &runtime{poolSize: envInt(envPoolSize, defaultPoolSize)}
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
// free-threading (--disable-gil) flag enabled.
func GilDisabled() bool { return gGilOff }

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

	return newModule(r, m, string(src))
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
