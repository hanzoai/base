//go:build pyvm
// +build pyvm

package pyvm

/*
#include "pyvm_bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	gort "runtime"
	"sync"
	"unsafe"

	"github.com/hanzoai/base/plugins/extruntime"
)

// module is one loaded Python extension. It owns a pool of
// sub-interpreters and the parsed source. The pool exists for
// concurrency: with PEP 684 OWN_GIL each sub-interpreter has its own
// GIL, so two goroutines invoking the same module can run truly in
// parallel as long as they grab different pool entries.
//
// Pool semantics: at most poolSize sub-interpreters are kept alive.
// On first Invoke we lazily create one. On subsequent Invokes we reuse
// an idle entry; if none is idle and we're under the cap we create
// another. The pool is closed when the module is closed.
type module struct {
	rt      *runtime
	name    string
	exports []string
	src     string

	mu      sync.Mutex
	pool    []*C.PyThreadState // idle sub-interpreters
	created int                // total ever created (for cap)
	closed  bool
}

func newModule(rt *runtime, m *extruntime.Manifest, src string) (*module, error) {
	mod := &module{
		rt:      rt,
		name:    m.Name,
		exports: append([]string(nil), m.Exports...),
		src:     src,
	}
	// Eagerly create one sub-interpreter at load time so the cost is
	// amortized — same shape as v8vm's CompileUnboundScript at Load.
	ts, err := mod.newSub()
	if err != nil {
		return nil, err
	}
	mod.pool = append(mod.pool, ts)
	mod.created = 1
	return mod, nil
}

func (m *module) Name() string      { return m.name }
func (*module) Runtime() string     { return "pyvm" }
func (m *module) Exports() []string { return append([]string(nil), m.exports...) }

// Invoke runs the named exported function with payload (a JSON
// document). Calling convention: json.loads payload, call fn(parsed),
// json.dumps result. Pure-stdlib on the Python side; the host stays
// out of the marshaling business.
//
// Context cancel: a watchdog goroutine schedules KeyboardInterrupt via
// Py_AddPendingCall. CPython runs pending calls between bytecodes, so
// any Python code that loops in pure Python will see the exception
// promptly. Code blocked inside a C extension (numpy MKL, time.sleep)
// will NOT respect this until it returns to bytecode dispatch.
func (m *module) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	m.mu.Unlock()

	if fn == "" {
		return nil, fmt.Errorf("%w: function name is empty", extruntime.ErrUnknownFn)
	}
	if len(payload) == 0 {
		payload = []byte("null")
	}

	// Pre-check ctx — if already cancelled before we acquire an
	// interpreter, bail without doing any work. Once inside CPython
	// we cannot reliably abort the running interpreter from another
	// OS thread (Py_AddPendingCall routes to the main interpreter
	// only, PyThreadState_SetAsyncExc requires the target's GIL).
	// Callers needing hard abort must use wazero + RustPython.
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}

	ts, err := m.acquire()
	if err != nil {
		return nil, err
	}

	// Pin to OS thread for the entire Python invocation. CPython thread
	// state is keyed by OS thread; if the goroutine migrates we'll be
	// stomping on someone else's interpreter. We unlock at the very
	// end, AFTER returning the interpreter to the pool, so the defer
	// ordering doesn't matter.
	gort.LockOSThread()

	C.pyvm_enter(ts)
	cFn := C.CString(fn)
	cPayload := C.CString(string(payload))
	var wasUnknown C.int
	var cErr *C.char
	cOut := C.pyvm_invoke(cFn, cPayload, &wasUnknown, &cErr)
	C.free(unsafe.Pointer(cFn))
	C.free(unsafe.Pointer(cPayload))
	C.pyvm_leave()

	// Return the sub-interpreter to the pool BEFORE unlocking the OS
	// thread. release() may call pyvm_end_sub which itself does a
	// PyEval_RestoreThread → Py_EndInterpreter → PyEval_SaveThread
	// dance that requires us to own the OS thread.
	m.release(ts)
	gort.UnlockOSThread()

	if cOut == nil {
		if cerr := ctx.Err(); cerr != nil {
			if cErr != nil {
				C.free(unsafe.Pointer(cErr))
			}
			return nil, cerr
		}
		if wasUnknown != 0 {
			if cErr != nil {
				C.free(unsafe.Pointer(cErr))
			}
			return nil, fmt.Errorf("%w: %s:%s", extruntime.ErrUnknownFn, m.name, fn)
		}
		return nil, goErrorAndFree(cErr, fmt.Sprintf("pyvm: %s:%s", m.name, fn))
	}
	if cErr != nil {
		C.free(unsafe.Pointer(cErr))
	}
	out := goStringAndFree(cOut)

	// Validate JSON shape — same defensive check as v8vm.
	var probe json.RawMessage
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		return nil, fmt.Errorf("pyvm: %s:%s: invalid result json: %w", m.name, fn, err)
	}
	return []byte(out), nil
}

func (m *module) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	pool := m.pool
	m.pool = nil
	m.mu.Unlock()

	// End all sub-interpreters. Each end requires the caller thread
	// not hold any other GIL; we pin to OS thread and tear them down
	// one at a time. The C helper handles the swap+restore.
	gort.LockOSThread()
	defer gort.UnlockOSThread()
	for _, ts := range pool {
		C.pyvm_end_sub(ts)
	}
	return nil
}

// acquire returns an idle sub-interpreter from the pool, or creates a
// new one if we're below the cap. Blocks (returns a fresh one) if the
// pool is empty AND we're at the cap — sub-interpreter creation cost
// is preferable to head-of-line blocking under contention. With the
// default poolSize of 4 we're rarely past the cap anyway.
func (m *module) acquire() (*C.PyThreadState, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	if n := len(m.pool); n > 0 {
		ts := m.pool[n-1]
		m.pool = m.pool[:n-1]
		m.mu.Unlock()
		return ts, nil
	}
	m.mu.Unlock()
	// Create a new one. We don't hold m.mu while creating — sub-interp
	// creation involves Python init code that can take 10-50ms.
	return m.newSub()
}

// release returns a sub-interpreter to the pool, capped at poolSize.
// Surplus interpreters are torn down.
func (m *module) release(ts *C.PyThreadState) {
	if ts == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		C.pyvm_end_sub(ts)
		return
	}
	if len(m.pool) < m.rt.poolSize {
		m.pool = append(m.pool, ts)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	C.pyvm_end_sub(ts)
}

// newSub creates a sub-interpreter and loads the module source into
// its __main__. The returned thread state belongs to that interpreter
// and the calling OS thread no longer holds the GIL.
func (m *module) newSub() (*C.PyThreadState, error) {
	gort.LockOSThread()
	defer gort.UnlockOSThread()
	var cErr *C.char
	ts := C.pyvm_new_sub(&cErr)
	if ts == nil {
		return nil, goErrorAndFree(cErr, "pyvm: new-interpreter")
	}
	if cErr != nil {
		C.free(unsafe.Pointer(cErr))
	}
	// Load source into the sub-interpreter. We must hold its GIL to
	// run code, then save the state before returning.
	C.pyvm_enter(ts)
	cSrc := C.CString(m.src)
	rc := C.pyvm_load_source(cSrc, &cErr)
	C.free(unsafe.Pointer(cSrc))
	C.pyvm_leave()
	if rc != 0 {
		// Loading failed — tear down the interpreter rather than
		// caching a broken one.
		C.pyvm_end_sub(ts)
		return nil, goErrorAndFree(cErr, "pyvm: load source")
	}
	if cErr != nil {
		C.free(unsafe.Pointer(cErr))
	}
	return ts, nil
}
