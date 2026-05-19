//go:build pyvm
// +build pyvm

// Package pyvm — C bridge implementation. See pyvm_bridge.h for the
// API contract and pyvm/README.md for the design notes.

#include "pyvm_bridge.h"
#include <stdlib.h>
#include <string.h>

static PyThreadState *gMainState = NULL;
static int gGilDisabled = 0;

PyThreadState* pyvm_init(void) {
    if (gMainState != NULL) return gMainState;
    Py_InitializeEx(0); // 0 = skip signal-handler install — Go owns signals.
#ifdef Py_GIL_DISABLED
    gGilDisabled = 1;
#else
    gGilDisabled = 0;
#endif
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

