// Package pyvm — C bridge declarations shared across pyvm cgo files.
// Implementation lives in pyvm_bridge.c so multiple .go files can call
// the same helpers without cgo dropping duplicate-symbol errors.

#ifndef PYVM_BRIDGE_H
#define PYVM_BRIDGE_H

#include <Python.h>

// pyvm_init initializes CPython (idempotent). Returns the saved main
// thread state, leaves the GIL released. Sets the global GIL-disabled
// flag retrievable via pyvm_gil_disabled.
PyThreadState* pyvm_init(void);

// pyvm_gil_disabled returns 1 if CPython was built with --disable-gil
// (free-threading PEP 703), 0 otherwise.
int pyvm_gil_disabled(void);

// pyvm_new_sub creates an OWN_GIL sub-interpreter (PEP 684). On
// success returns its thread state and leaves the caller thread
// without holding any GIL. On failure returns NULL and writes a heap-
// allocated error string to *err_out (caller frees).
PyThreadState* pyvm_new_sub(char **err_out);

// pyvm_end_sub destroys a sub-interpreter. Caller must NOT hold GIL.
// Safe to pass NULL.
void pyvm_end_sub(PyThreadState *ts);

// pyvm_enter / pyvm_leave swap the calling OS thread to/from the
// given sub-interpreter. Must be paired and called from the same OS
// thread.
void pyvm_enter(PyThreadState *ts);
void pyvm_leave(void);

// pyvm_load_source runs source as __main__ of the current
// sub-interpreter. Caller must hold the GIL. Returns 0 on success;
// on failure writes a heap-allocated error string to *err_out.
int pyvm_load_source(const char *src, char **err_out);

// pyvm_invoke calls __main__.fn(json.loads(payload_json)) and returns
// json.dumps(result) as a heap-allocated C string (caller frees).
// Caller must hold the GIL of the target sub-interpreter.
//
// On error returns NULL and writes the error to *err_out. If the
// failure is "function not found / not callable", *was_unknown is
// set to 1 — the Go layer maps that to ErrUnknownFn.
char* pyvm_invoke(const char *fn, const char *payload_json,
                  int *was_unknown, char **err_out);

// ------------------------------------------------------------------
// Threaded-mode bridge — single global interpreter, one OS thread
// per concurrent invocation. On a free-threaded (PEP 703) build, N
// goroutines pinned to N OS threads run Python bytecode truly in
// parallel; on a default-GIL build the GIL serializes them.
//
// All threaded-mode entry points use PyGILState_Ensure/Release to
// attach the calling OS thread to the global interpreter. They are
// safe to call from any goroutine that has previously called
// runtime.LockOSThread().
// ------------------------------------------------------------------

// pyvm_threaded_load loads `src` as a module identified by the
// mangled prefix `prefix`. For each top-level function defined in
// the module, a binding is created in __main__ under the mangled
// name "<prefix>__<fn>" so concurrent callers from different modules
// don't collide on the global symbol table.
//
// The function objects keep a reference to the module's own globals
// dict (NOT __main__), so module-private helpers continue to resolve
// against the module's namespace. Only the externally-visible
// entrypoints get mangled-and-published into __main__.
//
// Caller must NOT hold the GIL on entry. The bridge handles
// PyGILState_Ensure/Release internally. Returns 0 on success;
// on failure writes a heap-allocated error string to *err_out.
int pyvm_threaded_load(const char *prefix, const char *src,
                       char **err_out);

// pyvm_threaded_invoke calls the mangled-name function
// (e.g. "__pyvm_validate_email__validate") with the JSON payload.
// Same JSON-in / JSON-out contract as pyvm_invoke. Returns a heap-
// allocated C string (caller frees) on success.
//
// Caller must NOT hold the GIL on entry. The bridge handles
// PyGILState_Ensure/Release internally so the goroutine just
// LockOSThread + call + UnlockOSThread.
//
// On error returns NULL and writes the error to *err_out. If the
// failure is "function not found / not callable", *was_unknown is
// set to 1.
char* pyvm_threaded_invoke(const char *mangled_fn,
                           const char *payload_json,
                           int *was_unknown, char **err_out);

// pyvm_threaded_unload removes every binding in __main__ whose name
// starts with "<prefix>__" so an unloaded module doesn't leak symbols
// for the lifetime of the process. Caller must NOT hold the GIL.
void pyvm_threaded_unload(const char *prefix);

#endif // PYVM_BRIDGE_H
