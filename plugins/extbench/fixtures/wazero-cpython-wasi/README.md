# wazero-cpython-wasi fixture (DEFERRED)

CPython 3.13 compiled to `wasm32-wasi`, run inside wazero. Implements the
shared `validate(email, age)` contract.

## Status: DEFERRED (ABI mismatch, deferred shim)

The CPython WASI build produces a **WASI command module**: the guest
reads input from stdin and writes output to stdout via `_start`. Base's
`wasmvm` calling convention is **pointer-based**:
`validate(ptr, len) -> i64` packed result. Same mismatch as the
existing `wazero-javy` fixture.

Until a stdio→ptr/len shim lands, this benchmark is skipped and reports
the deferral in the comparison table.

### What was attempted (2026-05-18, time-boxed)

The realistic shim path requires either:
1. A 30 MB pre-built `python.wasm` from VMware Wasm Labs and authoring
   a separate Rust/AS wasm shim that wraps it (pipes payload bytes
   through CPython's stdio fds via `_start` and reads the result back),
   OR
2. Building CPython 3.13 from source against the WASI SDK (~15-20 min
   on M1) and forking its `_start` to add the pointer ABI directly.

Both are well outside the 60-min time-box for this benchmark pass.
RustPython was tried first (Rust → Rust embedding looked more
tractable); see `wazero-rustpython/README.md` for that outcome.

## Why deferred (not built today)

The realistic path requires:

1. A pre-built `python.wasm` (~30 MB) from VMware Wasm Labs / WASIX /
   CPython upstream, OR
2. Building CPython from source against the WASI SDK (~15-20 min on
   M1), AND
3. Either:
   - Authoring a Rust/AssemblyScript shim that wraps the CPython WASI
     entry, exposes `__base_alloc / __base_free / validate(ptr,len)`
     on top, and pipes the payload bytes through CPython's stdio fds, OR
   - Forking CPython's WASI entrypoint to add the pointer ABI directly.

Neither fits the ~3-4 hour budget for this benchmark pass. The
work item is captured below.

## Build path (when revisited)

```sh
# Option 1: pre-built python.wasm
# Check https://github.com/vmware-labs/webassembly-language-runtimes
# for the latest CPython release artifact, then drop python.wasm here.

# Option 2: build from source
cd /tmp
git clone --depth 1 --branch v3.13.5 https://github.com/python/cpython
cd cpython
./Tools/wasm/wasi-build.sh
cp builddir/wasi/python.wasm \
  ~/work/hanzo/base/plugins/extbench/fixtures/wazero-cpython-wasi/

# Then author the ptr/len shim (Rust target wasm32-wasi):
#   shim.rs exports __base_alloc, __base_free, validate(ptr,len)
#   internally:
#     - writes payload bytes into a WASI memfd backing stdin
#     - invokes _start of python.wasm
#     - reads stdout bytes back, allocates result in linear memory
#     - returns packed (resPtr<<32 | resLen) i64
```

Artifact size note: `python.wasm` is ~30 MB compressed (>50 MB
uncompressed). Commit via git-lfs or document the build step + leave
`python.wasm` `.gitignore`d. The fixture's manifest currently points
at `python.wasm`; the wasmvm runtime will report
`module python.wasm not built` and the bench harness will Skip the
runtime cleanly.

## Expected numbers (informed prediction)

Compared to pyvm (cgo CPython 3.13):

- Throughput: ~5-10x slower per invoke. WASI stdio round-trip adds
  microseconds per call; CPython startup itself is the same engine
  but the bytecode interpreter runs inside wasm linear memory which
  is ~2-3x slower than native pointer chasing in M1 caches.
- Per-module cost: dominated by the ~30 MB compiled `python.wasm`
  artifact shared across all instances. Per-instance footprint is
  the linear-memory growth from importing modules (~10-20 MB once
  json + sys are imported).
- Cold start: 100-500ms range. Lazy-loading from a `python.wasm`
  cold cache is the killer. Real deploys must warm-load.
- Hard sandbox: YES (this is the entire point — multi-tenant Python
  with crash isolation). Defeats pyvm's crash-isolation problem.

Decision rule for HIP-0105 / HIP-0106: pick wazero-cpython-wasi over
pyvm if and only if multi-tenant crash isolation is required AND the
~5-10x per-invoke overhead is acceptable AND the ~30 MB binary cost
is acceptable. Otherwise pyvm is the right answer for Python in single
tenant; goja or starlark for everything else.

## When this fixture becomes runnable

1. Drop `python.wasm` (or a wrapper.wasm that re-exports the
   pointer ABI) into this directory.
2. The wasmvm runtime auto-picks it up via the manifest at next
   `./run.sh`.
3. Update `docs/EXTENSIONS_BENCHMARK.md` "Full Cross-Runtime
   Comparison" section: drop the `skipped: ABI mismatch` cell, fill
   the actual numbers.
