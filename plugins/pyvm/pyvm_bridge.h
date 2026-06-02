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

#endif // PYVM_BRIDGE_H
