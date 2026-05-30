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
	"sync/atomic"
	"unsafe"

	"github.com/hanzoai/base/plugins/extruntime"
)

// module is one loaded Python extension. Behavior depends on m.mode:
//
//   ModeThreaded:
//     - No per-module sub-interpreters; m.pool stays nil.
//     - The module's source is loaded once into the SINGLE global
//       interpreter at construction time, with each exported function
//       published under a mangled symbol in __main__ so concurrent
//       callers don't collide on the global namespace.
//     - Invoke pins to OS thread, attaches via PyGILState_Ensure,
//       calls the mangled symbol, releases, unpins.
//
//   ModeSubinterp:
//     - Per-module pool of PEP 684 OWN_GIL sub-interpreters.
//     - On first Invoke we lazily create one; subsequent Invokes
//       reuse idle entries up to the cap (BASE_PYVM_POOL_SIZE).
//     - Each sub-interpreter has its own __main__ + the module source
//       loaded directly under the canonical export names.
//
// Pool semantics (subinterp only): at most poolSize sub-interpreters
// are kept alive. The pool is closed when the module is closed.
type module struct {
	rt      *runtime
	name    string
	exports []string
	src     string
	mode    Mode

	// threaded mode state
	manglePrefix string // unique per module instance, no trailing __

	// subinterp mode state
	mu      sync.Mutex
	pool    []*C.PyThreadState // idle sub-interpreters
	created int                // total ever created (for cap)
	closed  bool
}

// moduleSeq generates unique mangle-prefix suffixes so two
// concurrent Load()s of the same logical extension name don't share
// global symbols in threaded mode. Process-wide atomic.
var moduleSeq atomic.Uint64

func newModule(rt *runtime, m *extruntime.Manifest, src string, mode Mode) (*module, error) {
	mod := &module{
		rt:      rt,
		name:    m.Name,
		exports: append([]string(nil), m.Exports...),
		src:     src,
		mode:    mode,
	}

	switch mode {
	case ModeThreaded:
		mod.manglePrefix = manglePrefix(m.Name, moduleSeq.Add(1))
		if err := mod.loadThreaded(); err != nil {
			return nil, err
		}
		return mod, nil

	case ModeSubinterp:
		// Eagerly create one sub-interpreter at load time so the cost
		// is amortized — same shape as v8vm's CompileUnboundScript at
		// Load.
		ts, err := mod.newSub()
		if err != nil {
			return nil, err
		}
		mod.pool = append(mod.pool, ts)
		mod.created = 1
		return mod, nil

	default:
		return nil, fmt.Errorf("pyvm: unknown mode %v", mode)
	}
}

// manglePrefix derives a stable prefix for a module's exported
// functions in threaded mode. Format: "__pyvm_<sanitized-name>_<seq>".
// Caller guarantees the seq is unique within the process.
func manglePrefix(name string, seq uint64) string {
	// Replace anything that isn't a Python identifier char with '_'.
	// Python identifiers: letter | '_' followed by letter | digit | '_'.
	// Our use is just for the mangled global symbol — we don't have to
	// emit a syntactically valid identifier, but we want the symbol
	// to be unambiguous when grepped, so stick to [A-Za-z0-9_].
	b := make([]byte, 0, 8+len(name)+16)
	b = append(b, "__pyvm_"...)
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	b = append(b, '_')
	// uint64 in base-10 — keeps the prefix readable; max 20 digits.
	var seqBuf [20]byte
	si := len(seqBuf)
	x := seq
	if x == 0 {
		si--
		seqBuf[si] = '0'
	} else {
		for x > 0 {
			si--
			seqBuf[si] = byte('0' + x%10)
			x /= 10
		}
	}
	b = append(b, seqBuf[si:]...)
	return string(b)
}

func (m *module) Name() string      { return m.name }
func (m *module) Mode() Mode        { return m.mode }
func (*module) Runtime() string     { return "pyvm" }
func (m *module) Exports() []string { return append([]string(nil), m.exports...) }

// Invoke runs the named exported function with payload (a JSON
// document). Calling convention: json.loads payload, call fn(parsed),
// json.dumps result. Pure-stdlib on the Python side; the host stays
// out of the marshaling business.
//
// Context cancel: a pre-check at the top fires ctx.Err() before any
// Python work. Once inside CPython we cannot reliably abort the
// running interpreter from another OS thread (Py_AddPendingCall
// routes to the main interpreter only, PyThreadState_SetAsyncExc
// requires the target's GIL). Callers needing hard abort must use
// wazero + RustPython.
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
	// interpreter, bail without doing any work.
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}

	switch m.mode {
	case ModeThreaded:
		return m.invokeThreaded(ctx, fn, payload)
	case ModeSubinterp:
		return m.invokeSubinterp(ctx, fn, payload)
	default:
		return nil, fmt.Errorf("pyvm: unknown mode %v", m.mode)
	}
}

// invokeThreaded calls into the global interpreter via
// PyGILState_Ensure. On a free-threaded build, N goroutines on N OS
// threads execute Python bytecode truly in parallel.
func (m *module) invokeThreaded(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	// LockOSThread is required even on free-threaded builds: PyGILState_*
	// attaches CPython's thread state to the calling OS thread.
	// Migrating the goroutine to a different OS thread between Ensure
	// and Release would stomp on someone else's state.
	gort.LockOSThread()
	defer gort.UnlockOSThread()

	mangled := m.manglePrefix + "__" + fn
	cFn := C.CString(mangled)
	cPayload := C.CString(string(payload))
	var wasUnknown C.int
	var cErr *C.char
	cOut := C.pyvm_threaded_invoke(cFn, cPayload, &wasUnknown, &cErr)
	C.free(unsafe.Pointer(cFn))
	C.free(unsafe.Pointer(cPayload))

	return m.finishInvoke(ctx, fn, cOut, wasUnknown, cErr)
}

// invokeSubinterp acquires a sub-interpreter from the per-module pool
// and runs the function against its private __main__. This is the
// legacy execution path; opt in via extension.json "mode":"subinterp"
// or BASE_PYVM_MODE=subinterp.
func (m *module) invokeSubinterp(ctx context.Context, fn string, payload []byte) ([]byte, error) {
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

	return m.finishInvoke(ctx, fn, cOut, wasUnknown, cErr)
}

// finishInvoke is the common tail: error mapping, ctx re-check, JSON
// validation. Both modes hand it the same C-side return shape so the
// post-processing stays in one place.
func (m *module) finishInvoke(ctx context.Context, fn string, cOut *C.char, wasUnknown C.int, cErr *C.char) ([]byte, error) {
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
	mode := m.mode
	prefix := m.manglePrefix
	m.mu.Unlock()

	switch mode {
	case ModeThreaded:
		if prefix != "" {
			cPrefix := C.CString(prefix)
			C.pyvm_threaded_unload(cPrefix)
			C.free(unsafe.Pointer(cPrefix))
		}
		return nil

	case ModeSubinterp:
		// End all sub-interpreters. Each end requires the caller
		// thread not hold any other GIL; we pin to OS thread and
		// tear them down one at a time. The C helper handles the
		// swap+restore.
		gort.LockOSThread()
		defer gort.UnlockOSThread()
		for _, ts := range pool {
			C.pyvm_end_sub(ts)
		}
		return nil

	default:
		return nil
	}
}

// loadThreaded calls the C bridge to exec the module source under a
// mangled prefix in the global interpreter. Idempotent across re-load
// because each Load() bumps moduleSeq and gets a new prefix.
func (m *module) loadThreaded() error {
	cPrefix := C.CString(m.manglePrefix)
	cSrc := C.CString(m.src)
	var cErr *C.char
	rc := C.pyvm_threaded_load(cPrefix, cSrc, &cErr)
	C.free(unsafe.Pointer(cPrefix))
	C.free(unsafe.Pointer(cSrc))
	if rc != 0 {
		return goErrorAndFree(cErr, "pyvm: load threaded "+m.name)
	}
	if cErr != nil {
		C.free(unsafe.Pointer(cErr))
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
