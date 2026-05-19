//go:build pyvm
// +build pyvm

// Package pyvm — C bridge implementation. See pyvm_bridge.h for the
// API contract and pyvm/README.md for the design notes.

#include "pyvm_bridge.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static PyThreadState *gMainState = NULL;
static int gGilDisabled = 0;
// Per-OS-thread cache of PyThreadState* for threaded-mode invokes.
// First Invoke on an OS thread allocates a fresh ts in the main
// interpreter; subsequent Invokes on the SAME OS thread reuse it.
// The ts stays in the interpreter's thread list for the lifetime of
// the process; we never destroy it. (The OS thread might die first,
// but the dangling ts in the linked list is harmless — nobody calls
// PyEval_AcquireThread on it.)
//
// This is the proper-perf path. The alternative — PyThreadState_New
// every call — measured at ~400 µs/call on FT 3.13t due to
// HEAD_LOCK contention and the qsbr_register pass. Caching collapses
// the per-call cost to a TSS lookup + AcquireThread (~200 ns on FT).
static Py_tss_t gThreadedTSS = Py_tss_NEEDS_INIT;

PyThreadState* pyvm_init(void) {
    if (gMainState != NULL) return gMainState;
    Py_InitializeEx(0); // 0 = skip signal-handler install — Go owns signals.
#ifdef Py_GIL_DISABLED
    // On a free-threaded build (PEP 703) the GIL never engages, so
    // PyEval_AcquireThread becomes a thread-state attach with no
    // wait. Detect at compile time — Py_GIL_DISABLED is set by the
    // cpython build when --disable-gil was passed to configure.
    gGilDisabled = 1;
#else
    gGilDisabled = 0;
#endif
    // Allocate the per-OS-thread TSS key BEFORE releasing the GIL —
    // PyThread_tss_create is documented as safe outside the GIL but
    // we hold it here anyway. Failure is fatal (no memory left).
    if (PyThread_tss_is_created(&gThreadedTSS) == 0) {
        if (PyThread_tss_create(&gThreadedTSS) != 0) {
            // Cannot raise — we don't have a tstate object yet from
            // the caller's POV. Stuff the failure into stderr.
            fprintf(stderr, "pyvm: PyThread_tss_create failed\n");
        }
    }
    gMainState = PyEval_SaveThread(); // release GIL, return main state
    return gMainState;
}

int pyvm_gil_disabled(void) { return gGilDisabled; }

PyThreadState* pyvm_new_sub(char **err_out) {
    PyEval_RestoreThread(gMainState); // acquire GIL on main interp
    PyInterpreterConfig cfg = {
        .use_main_obmalloc = 0,
        .allow_fork = 0,
        .allow_exec = 0,
        .allow_threads = 1,
        .allow_daemon_threads = 0,
        .check_multi_interp_extensions = 1,
        .gil = PyInterpreterConfig_OWN_GIL,
    };
    PyThreadState *ts = NULL;
    PyStatus st = Py_NewInterpreterFromConfig(&ts, &cfg);
    if (PyStatus_Exception(st)) {
        if (err_out) {
            const char *msg = st.err_msg ? st.err_msg : "Py_NewInterpreterFromConfig failed";
            *err_out = strdup(msg);
        }
        if (PyThreadState_Get() != gMainState) {
            PyThreadState_Swap(gMainState);
        }
        gMainState = PyEval_SaveThread();
        return NULL;
    }
    PyEval_SaveThread();
    return ts;
}

void pyvm_end_sub(PyThreadState *ts) {
    if (ts == NULL) return;
    PyEval_RestoreThread(ts);
    Py_EndInterpreter(ts);
    PyThreadState_Swap(gMainState);
    PyEval_SaveThread();
}

void pyvm_enter(PyThreadState *ts) {
    PyEval_RestoreThread(ts);
}

void pyvm_leave(void) {
    PyEval_SaveThread();
}

int pyvm_load_source(const char *src, char **err_out) {
    PyObject *mainMod = PyImport_AddModule("__main__"); // borrowed
    if (mainMod == NULL) goto err;
    PyObject *globals = PyModule_GetDict(mainMod); // borrowed
    if (globals == NULL) goto err;
    PyObject *r = PyRun_String(src, Py_file_input, globals, globals);
    if (r == NULL) goto err;
    Py_DECREF(r);
    PyObject *json = PyImport_ImportModule("json");
    if (json == NULL) goto err;
    if (PyDict_SetItemString(globals, "_pyvm_json", json) < 0) {
        Py_DECREF(json);
        goto err;
    }
    Py_DECREF(json);
    return 0;
err:
    if (err_out) {
        PyObject *type, *value, *tb;
        PyErr_Fetch(&type, &value, &tb);
        PyObject *s = value ? PyObject_Str(value) : NULL;
        const char *cstr = s ? PyUnicode_AsUTF8(s) : "python load error";
        *err_out = strdup(cstr ? cstr : "python load error");
        Py_XDECREF(s);
        Py_XDECREF(type);
        Py_XDECREF(value);
        Py_XDECREF(tb);
        PyErr_Clear();
    }
    return -1;
}

char* pyvm_invoke(const char *fn, const char *payload_json,
                  int *was_unknown, char **err_out) {
    *was_unknown = 0;
    PyObject *mainMod = PyImport_AddModule("__main__");
    if (mainMod == NULL) goto err;
    PyObject *globals = PyModule_GetDict(mainMod);
    if (globals == NULL) goto err;
    PyObject *fnObj = PyDict_GetItemString(globals, fn);
    if (fnObj == NULL || !PyCallable_Check(fnObj)) {
        *was_unknown = 1;
        if (err_out) *err_out = strdup("function not found");
        return NULL;
    }
    PyObject *jsonMod = PyDict_GetItemString(globals, "_pyvm_json");
    if (jsonMod == NULL) goto err;
    PyObject *loads = PyObject_GetAttrString(jsonMod, "loads");
    if (loads == NULL) goto err;
    PyObject *dumps = PyObject_GetAttrString(jsonMod, "dumps");
    if (dumps == NULL) { Py_DECREF(loads); goto err; }
    PyObject *payloadStr = PyUnicode_FromString(payload_json);
    if (payloadStr == NULL) { Py_DECREF(loads); Py_DECREF(dumps); goto err; }
    PyObject *parsed = PyObject_CallOneArg(loads, payloadStr);
    Py_DECREF(payloadStr);
    Py_DECREF(loads);
    if (parsed == NULL) { Py_DECREF(dumps); goto err; }
    PyObject *result = PyObject_CallOneArg(fnObj, parsed);
    Py_DECREF(parsed);
    if (result == NULL) { Py_DECREF(dumps); goto err; }
    PyObject *resultStr = PyObject_CallOneArg(dumps, result);
    Py_DECREF(result);
    Py_DECREF(dumps);
    if (resultStr == NULL) goto err;
    const char *cstr = PyUnicode_AsUTF8(resultStr);
    if (cstr == NULL) { Py_DECREF(resultStr); goto err; }
    char *out = strdup(cstr);
    Py_DECREF(resultStr);
    return out;
err:
    if (err_out) {
        PyObject *type, *value, *tb;
        PyErr_Fetch(&type, &value, &tb);
        PyObject *s = value ? PyObject_Str(value) : NULL;
        const char *cstr = s ? PyUnicode_AsUTF8(s) : "python invoke error";
        *err_out = strdup(cstr ? cstr : "python invoke error");
        Py_XDECREF(s);
        Py_XDECREF(type);
        Py_XDECREF(value);
        Py_XDECREF(tb);
        PyErr_Clear();
    }
    return NULL;
}

// ------------------------------------------------------------------
// Threaded-mode helpers
// ------------------------------------------------------------------
//
// Module loading in threaded mode:
//
//   1. Build a fresh dict to serve as the module's namespace.
//   2. exec(src) inside that dict — module-private helpers, classes,
//      and constants live there.
//   3. For each top-level callable defined in that dict, publish a
//      reference into __main__ under "<prefix>__<name>". The callable
//      keeps its closure over its own globals dict, so private helpers
//      resolve correctly even though the function is "called from"
//      __main__.
//   4. Stash one shared json module reference in __main__ as
//      _pyvm_json (idempotent).
//
// Symbol convention: prefix = sanitized module name with "__pyvm_"
// front and "__" tail. The Go side guarantees prefix is unique per
// loaded module instance.
//
// Concurrency safety:
//   - We DO NOT use PyGILState_Ensure here. PyGILState_* relies on an
//     autoTSSkey that, when sub-interpreters exist in the same process,
//     can attach a thread to the wrong interpreter (whichever sub-
//     interp last touched this OS thread). The result is a fatal
//     "_PyThreadState_Attach: non-NULL old thread state" or worse.
//   - Instead, every threaded-mode call:
//       a. Allocates a fresh PyThreadState in the MAIN interpreter
//          via PyThreadState_New(main_interp).
//       b. PyEval_AcquireThread(ts) — swaps in + (on default-GIL)
//          waits for the GIL.
//       c. Does the work.
//       d. PyEval_ReleaseThread(ts).
//       e. PyThreadState_Clear + PyThreadState_Delete to drop the ts.
//   - On a free-threaded build, AcquireThread doesn't block; multiple
//     OS threads can each own a thread state in the main interpreter
//     simultaneously and execute bytecode in true parallel.
//
// Helper:

// threaded_attach attaches the calling OS thread to the MAIN
// interpreter. Uses a per-OS-thread TSS cache: first call on a
// thread allocates a fresh PyThreadState; subsequent calls reuse it.
//
// Caller MUST pair with threaded_detach. Caller MUST hold an OS
// thread via runtime.LockOSThread on the Go side.
//
// Why a custom TSS instead of PyGILState_Ensure?
//
// PyGILState_Ensure relies on the runtime's autoTSSkey that can be
// left in an inconsistent state by sub-interpreter teardown (after
// pyvm_end_sub the OS thread's autoTSSkey can point at the destroyed
// sub-interp's ts, triggering "_PyThreadState_Attach: non-NULL old
// thread state" on the next GILState_Ensure).
//
// Our gThreadedTSS is independent: each main-interp ts we create
// is kept in the interpreter's thread list FOR THE LIFETIME OF
// THE PROCESS, and we look it up by current OS thread on every
// Invoke. Lookup is a single TLS access (~10 ns); attach is
// PyEval_AcquireThread (~100-200 ns on FT, blocking wait on
// default-GIL).
//
// Memory: each OS thread that ever invoked uses ~1 KB for its
// cached ts. The Go runtime caps M's at GOMAXPROCS + cgo callers,
// so the total is bounded by the M cap (typically <100). On
// process exit Python tears down the interp and frees them all.

typedef struct {
    PyThreadState *ts;
} pyvm_threaded_handle;

// fresh_threadstate creates a new ts in the main interpreter and
// stores it in the per-OS-thread TSS. Caller does NOT hold the GIL.
static PyThreadState *fresh_threadstate(void) {
    if (gMainState == NULL || gMainState->interp == NULL) return NULL;
    PyThreadState *ts = PyThreadState_New(gMainState->interp);
    if (ts == NULL) return NULL;
    if (PyThread_tss_set(&gThreadedTSS, (void *)ts) != 0) {
        // tss_set can't really fail post-create, but be defensive.
        PyThreadState_Clear(ts);
        PyThreadState_Delete(ts);
        return NULL;
    }
    return ts;
}

static pyvm_threaded_handle threaded_attach(void) {
    pyvm_threaded_handle h = {NULL};
    PyThreadState *ts = (PyThreadState *)PyThread_tss_get(&gThreadedTSS);
    if (ts == NULL) {
        ts = fresh_threadstate();
        if (ts == NULL) return h;
    }
    PyEval_AcquireThread(ts);
    h.ts = ts;
    return h;
}

static void threaded_detach(pyvm_threaded_handle h) {
    if (h.ts == NULL) return;
    PyEval_ReleaseThread(h.ts);
}

// Helper: build a fresh module namespace and exec(src) into it.
// Returns a new strong reference to the populated dict on success,
// NULL on error (with err_out populated).
static PyObject *threaded_exec_into_fresh_dict(const char *src,
                                               char **err_out) {
    PyObject *mod_globals = PyDict_New();
    if (mod_globals == NULL) {
        if (err_out) *err_out = strdup("PyDict_New failed");
        return NULL;
    }
    // Standard __builtins__ injection so the exec'd source can call
    // print(), len(), isinstance(), etc.
    PyObject *builtins = PyEval_GetBuiltins(); // borrowed
    if (builtins != NULL) {
        if (PyDict_SetItemString(mod_globals, "__builtins__", builtins) < 0) {
            Py_DECREF(mod_globals);
            if (err_out) *err_out = strdup("inject builtins failed");
            return NULL;
        }
    }
    PyObject *r = PyRun_String(src, Py_file_input, mod_globals, mod_globals);
    if (r == NULL) {
        Py_DECREF(mod_globals);
        if (err_out) {
            PyObject *type, *value, *tb;
            PyErr_Fetch(&type, &value, &tb);
            PyObject *s = value ? PyObject_Str(value) : NULL;
            const char *cstr = s ? PyUnicode_AsUTF8(s) : "exec failed";
            *err_out = strdup(cstr ? cstr : "exec failed");
            Py_XDECREF(s);
            Py_XDECREF(type);
            Py_XDECREF(value);
            Py_XDECREF(tb);
            PyErr_Clear();
        }
        return NULL;
    }
    Py_DECREF(r);
    return mod_globals; // caller owns
}

int pyvm_threaded_load(const char *prefix, const char *src,
                       char **err_out) {
    if (prefix == NULL || src == NULL) {
        if (err_out) *err_out = strdup("nil prefix/src");
        return -1;
    }
    pyvm_threaded_handle h = threaded_attach();
    if (h.ts == NULL) {
        if (err_out) *err_out = strdup("threaded_attach failed");
        return -1;
    }

    PyObject *mod_globals = threaded_exec_into_fresh_dict(src, err_out);
    if (mod_globals == NULL) {
        threaded_detach(h);
        return -1;
    }

    PyObject *mainMod = PyImport_AddModule("__main__"); // borrowed
    if (mainMod == NULL) {
        Py_DECREF(mod_globals);
        if (err_out) *err_out = strdup("PyImport_AddModule(__main__) failed");
        threaded_detach(h);
        return -1;
    }
    PyObject *mainGlobals = PyModule_GetDict(mainMod); // borrowed
    if (mainGlobals == NULL) {
        Py_DECREF(mod_globals);
        if (err_out) *err_out = strdup("PyModule_GetDict(__main__) failed");
        threaded_detach(h);
        return -1;
    }

    // Idempotently stash json on __main__ as _pyvm_json. The threaded-
    // invoke path reaches into __main__ for it on every call.
    if (PyDict_GetItemString(mainGlobals, "_pyvm_json") == NULL) {
        PyObject *json = PyImport_ImportModule("json");
        if (json == NULL) {
            Py_DECREF(mod_globals);
            if (err_out) *err_out = strdup("import json failed");
            threaded_detach(h);
            return -1;
        }
        if (PyDict_SetItemString(mainGlobals, "_pyvm_json", json) < 0) {
            Py_DECREF(json);
            Py_DECREF(mod_globals);
            if (err_out) *err_out = strdup("publish _pyvm_json failed");
            threaded_detach(h);
            return -1;
        }
        Py_DECREF(json);
    }

    // Walk the module's globals; copy every top-level callable that
    // doesn't start with "_" under a mangled name in __main__.
    PyObject *keys = PyDict_Keys(mod_globals);
    if (keys == NULL) {
        Py_DECREF(mod_globals);
        if (err_out) *err_out = strdup("PyDict_Keys(mod_globals) failed");
        threaded_detach(h);
        return -1;
    }
    Py_ssize_t n = PyList_Size(keys);
    for (Py_ssize_t i = 0; i < n; i++) {
        PyObject *k = PyList_GetItem(keys, i); // borrowed
        if (!PyUnicode_Check(k)) continue;
        const char *name = PyUnicode_AsUTF8(k);
        if (name == NULL || name[0] == '_') continue;
        PyObject *v = PyDict_GetItem(mod_globals, k); // borrowed
        if (v == NULL || !PyCallable_Check(v)) continue;
        // Build "<prefix>__<name>"
        char mangled[512];
        int wrote = snprintf(mangled, sizeof(mangled), "%s__%s",
                             prefix, name);
        if (wrote < 0 || (size_t)wrote >= sizeof(mangled)) continue;
        if (PyDict_SetItemString(mainGlobals, mangled, v) < 0) {
            Py_DECREF(keys);
            Py_DECREF(mod_globals);
            if (err_out) *err_out = strdup("publish mangled name failed");
            threaded_detach(h);
            return -1;
        }
    }
    Py_DECREF(keys);
    // mod_globals is anchored by every published function's __globals__
    // attribute, so we drop our reference here; the callables keep
    // their closure references alive.
    Py_DECREF(mod_globals);

    threaded_detach(h);
    return 0;
}

char* pyvm_threaded_invoke(const char *mangled_fn,
                           const char *payload_json,
                           int *was_unknown, char **err_out) {
    if (was_unknown != NULL) *was_unknown = 0;
    pyvm_threaded_handle h = threaded_attach();
    if (h.ts == NULL) {
        if (err_out) *err_out = strdup("threaded_attach failed");
        return NULL;
    }

    PyObject *mainMod = PyImport_AddModule("__main__"); // borrowed
    if (mainMod == NULL) goto err;
    PyObject *globals = PyModule_GetDict(mainMod); // borrowed
    if (globals == NULL) goto err;
    PyObject *fnObj = PyDict_GetItemString(globals, mangled_fn); // borrowed
    if (fnObj == NULL || !PyCallable_Check(fnObj)) {
        if (was_unknown != NULL) *was_unknown = 1;
        if (err_out) *err_out = strdup("function not found");
        threaded_detach(h);
        return NULL;
    }
    PyObject *jsonMod = PyDict_GetItemString(globals, "_pyvm_json"); // borrowed
    if (jsonMod == NULL) goto err;
    PyObject *loads = PyObject_GetAttrString(jsonMod, "loads");
    if (loads == NULL) goto err;
    PyObject *dumps = PyObject_GetAttrString(jsonMod, "dumps");
    if (dumps == NULL) { Py_DECREF(loads); goto err; }
    PyObject *payloadStr = PyUnicode_FromString(payload_json);
    if (payloadStr == NULL) { Py_DECREF(loads); Py_DECREF(dumps); goto err; }
    PyObject *parsed = PyObject_CallOneArg(loads, payloadStr);
    Py_DECREF(payloadStr);
    Py_DECREF(loads);
    if (parsed == NULL) { Py_DECREF(dumps); goto err; }
    PyObject *result = PyObject_CallOneArg(fnObj, parsed);
    Py_DECREF(parsed);
    if (result == NULL) { Py_DECREF(dumps); goto err; }
    PyObject *resultStr = PyObject_CallOneArg(dumps, result);
    Py_DECREF(result);
    Py_DECREF(dumps);
    if (resultStr == NULL) goto err;
    const char *cstr = PyUnicode_AsUTF8(resultStr);
    if (cstr == NULL) { Py_DECREF(resultStr); goto err; }
    char *out = strdup(cstr);
    Py_DECREF(resultStr);
    threaded_detach(h);
    return out;
err:
    if (err_out) {
        PyObject *type, *value, *tb;
        PyErr_Fetch(&type, &value, &tb);
        PyObject *s = value ? PyObject_Str(value) : NULL;
        const char *cstr = s ? PyUnicode_AsUTF8(s) : "python invoke error";
        *err_out = strdup(cstr ? cstr : "python invoke error");
        Py_XDECREF(s);
        Py_XDECREF(type);
        Py_XDECREF(value);
        Py_XDECREF(tb);
        PyErr_Clear();
    }
    threaded_detach(h);
    return NULL;
}

void pyvm_threaded_unload(const char *prefix) {
    if (prefix == NULL) return;
    pyvm_threaded_handle h = threaded_attach();
    PyObject *mainMod = PyImport_AddModule("__main__");
    if (mainMod == NULL) {
        PyErr_Clear();
        threaded_detach(h);
        return;
    }
    PyObject *globals = PyModule_GetDict(mainMod);
    if (globals == NULL) {
        PyErr_Clear();
        threaded_detach(h);
        return;
    }
    // Walk a snapshot of keys; mutate dict from the snapshot list.
    PyObject *keys = PyDict_Keys(globals);
    if (keys == NULL) {
        PyErr_Clear();
        threaded_detach(h);
        return;
    }
    size_t plen = strlen(prefix);
    Py_ssize_t n = PyList_Size(keys);
    for (Py_ssize_t i = 0; i < n; i++) {
        PyObject *k = PyList_GetItem(keys, i); // borrowed
        if (!PyUnicode_Check(k)) continue;
        const char *name = PyUnicode_AsUTF8(k);
        if (name == NULL) continue;
        if (strncmp(name, prefix, plen) != 0) continue;
        // Make sure the next chars are "__" so we don't accidentally
        // delete an unrelated symbol that happens to share a prefix.
        if (name[plen] != '_' || name[plen+1] != '_') continue;
        PyDict_DelItem(globals, k);
    }
    Py_DECREF(keys);
    PyErr_Clear();
    threaded_detach(h);
}

