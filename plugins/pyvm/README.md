# plugins/pyvm — CPython extension runtime for Base

`pyvm` is the cgo-based CPython 3.13 extension runtime. It satisfies the
`extruntime.Runtime` and `extruntime.Module` interfaces from
`plugins/extruntime`, sitting alongside the other four engines (native,
goja, wazero, v8go).

Default build links the stub (`stub.go`), which errors at `Load` time so
applications that don't need Python don't pay the libpython link cost
or the build-toolchain requirement.

## Build

The real implementation lives behind the `pyvm` build tag and requires
Python 3.13 development headers + the embed pkg-config file:

```bash
# macOS (homebrew)
brew install python@3.13
PKG_CONFIG_PATH=/opt/homebrew/opt/python@3.13/lib/pkgconfig \
  go build -tags pyvm ./...

# Debian / Ubuntu
sudo apt-get install python3.13-dev
go build -tags pyvm ./...

# Free-threaded build (PEP 703)
brew install python@3.13t    # or compile cpython with --disable-gil
PKG_CONFIG_PATH=/opt/homebrew/opt/python@3.13t/lib/pkgconfig \
  go build -tags pyvm ./...
```

Without `-tags pyvm`, the stub is linked and `NewRuntime().Load()`
returns `ErrUnsupported`. Capabilities (`Cgo`, language list) still
report correctly so callers can branch on availability.

## Architecture

```
process
└── CPython runtime (Py_InitializeEx once, global)
    ├── main interpreter        ← we don't run user code here
    ├── module A sub-interp #0  ← OWN_GIL, holds module A source
    ├── module A sub-interp #1  ← OWN_GIL, parallel slot for module A
    ├── module B sub-interp #0  ← OWN_GIL, holds module B source
    └── …
```

Per-module sub-interpreter pool, OWN_GIL (PEP 684) on Python 3.12+,
which means two concurrent Invokes on the same module can run on
different OS threads in true parallel — each interpreter has its own
GIL.

On a free-threaded build (`Py_GIL_DISABLED=1`, PEP 703), the GIL is
a no-op and pool entries run truly in parallel regardless of OWN_GIL.

### Invocation contract

`Invoke(ctx, fn, payloadJSON)` does:

1. `runtime.LockOSThread()` — pin to one OS thread.
2. Acquire a sub-interpreter from the module's pool (lazy create up to
   `BASE_PYVM_POOL_SIZE`, default 4).
3. `PyEval_RestoreThread(ts)` — attach the sub-interp's thread state
   and acquire its GIL on this OS thread.
4. Call `__main__.fn(json.loads(payloadJSON))`.
5. `json.dumps(result)` → return bytes.
6. `PyEval_SaveThread()` — release the GIL.
7. Return the interpreter to the pool (or tear it down if surplus).
8. `runtime.UnlockOSThread()`.

JSON marshaling happens **inside** the sub-interpreter via Python's
stdlib `json` module — the Go side never builds Python dicts/lists.
This keeps the cgo surface tiny and the cost portable.

### Cancellation — honest limitations

`Invoke(ctx, …)` returns `ctx.Err()` if the context is already
cancelled at the moment we acquire the interpreter. **Once Python is
running, we cannot abort it from another OS thread:**

- `Py_AddPendingCall` routes to the main interpreter only.
- `PyThreadState_SetAsyncExc(tid, exc)` requires the target's GIL,
  which the running Invoke holds.
- `PyErr_SetInterrupt` only fires via the SIGINT handler in the main
  thread.

For hooks that may run long, callers should:
- Set their own internal deadline inside the Python code (e.g.
  `time.monotonic()` polling).
- Or use a different runtime: **wazero + RustPython / pyodide** gives
  you hard abort + a real sandbox at the cost of pure-Python perf.

`Capabilities().SupportsAbort = false` reflects this honestly.

### Crash isolation — honest limitations

`Capabilities().HardSandbox = false`. A C extension segfault crashes
the whole Go process. Per HIP-0105, pyvm is **single-tenant deploys
only** — never load untrusted Python in a multi-tenant pyvm host.

For multi-tenant Python, the supported path is wazero with a
Python-to-wasm artifact (RustPython, py2wasm, or CPython compiled to
wasm32-wasi).

## Configuration

| Env var | Default | Description |
|---|---|---|
| `BASE_PYVM_POOL_SIZE` | `4` | Max sub-interpreters retained per module. Increasing this raises memory but enables more parallel invocations of the same module. |

## Module manifest

```json
{
  "name": "validate-email",
  "version": "0.1.0",
  "runtime": "pyvm",
  "module": "validate.py",
  "exports": ["validate"]
}
```

The Python source must define top-level functions matching the
`exports` list. Each function takes one argument (the JSON-parsed
payload) and returns a JSON-serializable value.

## When to use pyvm

- Single-tenant deployments running trusted code, where Python's
  ecosystem (numpy, pandas, transformers, sympy, …) is needed.
- ML inference hooks where the cost of Python startup is amortized
  across many invocations.
- Cases where the same logic exists in Python and porting to JS/wasm
  costs more than it saves.

## When NOT to use pyvm

- Multi-tenant production. Use wazero + RustPython.
- Any environment where a tenant's misbehaving code must not crash
  the host. Use wazero (hard sandbox) or v8go (process-level isolation
  with the abort caveat).
- Builds that must not link libpython (small container images, no-cgo
  builds). The default stub build is exactly this.

## Benchmark numbers

See `docs/EXTENSIONS_BENCHMARK.md` for the full numbers. Headline
(darwin/arm64, Apple M1 Max, Python 3.13.13, GIL build):

- Serial: 4012 ns/op, 496 B/op, 10 allocs/op
- Parallel: 2765 ns/op (4-interp pool wins via OWN_GIL parallelism)
- Cold load: 14 ms/module (sub-interpreter creation cost)
- Steady-state: 18 µs/invoke after warmup

Verdict: faster than v8go (4x serial, 4x parallel), competitive with
goja, slower than native Go. Cold start is the worst in the field —
deploy with warmups.

## Files

- `stub.go` — default build, no Python required
- `runtime.go`, `module.go` — `+build pyvm` Go side
- `pyvm_bridge.h`, `pyvm_bridge.c` — `+build pyvm` shared C glue
- `runtime_test.go` — `+build pyvm` table tests
