# wazero-rustpython fixture (DEFERRED)

[RustPython](https://github.com/RustPython/RustPython), a Rust implementation
of Python 3.12-ish, compiled to `wasm32-wasi` and run inside wazero.
Smaller than CPython-WASI (~5-10 MB vs ~30 MB) and faster cold start.

## Status: DEFERRED (ABI shim attempted, time-boxed)

Same WASI-command-vs-pointer-ABI mismatch as the CPython-WASI fixture.
RustPython emits a WASI command module — guest reads stdin, writes
stdout via `_start`. Base's wasmvm wants `__base_alloc / __base_free /
validate(ptr,len) -> i64`.

### What was attempted (2026-05-18, ~60min budget)

The plausible path was to author a tiny Rust shim crate that pulls in
`rustpython-vm` as a dep, exposes the pointer ABI as `__base_alloc /
__base_free / validate(ptr,len) -> i64`, and runs the Python `validate`
function in-process via `rustpython_vm::eval::eval`:

```rust
use rustpython_vm as vm;

#[no_mangle]
pub extern "C" fn validate(ptr: i32, len: i32) -> i64 {
    let interp = vm::Interpreter::without_stdlib(Default::default());
    interp.enter(|vm_ref| {
        let scope = vm_ref.new_scope_with_builtins();
        // ... run validate.py source via vm::eval::eval ...
    })
}
```

Build target: `wasm32-wasip1` with
`rustpython-vm = { version = "0.5", features = ["freeze-stdlib", "rustpython-compiler"] }`.

### Why deferred

1. `rustpython-vm` 0.5.0 fails to compile when both `freeze-stdlib`
   and `rustpython-compiler` features are enabled on `wasm32-wasip1` —
   11 type errors surface from the `define_exception_fn!` macro
   interaction with the wasm cfg path. Resolving requires either
   patching the crate (out of budget) or pinning to an older revision
   (no compatible 0.4.x exists with both features on stable Rust).
2. Each rebuild after a fix attempt is ~60s of cargo time; the
   feedback loop ate the budget.

The right next step is to vendor a minimal `rustpython-vm` fork that
exposes `eval()` without the `define_exception_fn!` macro, OR to wait
for upstream 0.6.x.

### Build path (when revisited)

```sh
# Install rustup + wasm32-wasip1 target.
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --no-modify-path --profile minimal
. "$HOME/.cargo/env"
rustup target add wasm32-wasip1

# Author the shim crate next to validate.py:
#   shim/Cargo.toml: rustpython-vm dep with the right feature combo
#   shim/src/lib.rs: pointer ABI + eval(validate.py source)
cargo build --release --target wasm32-wasip1
cp target/wasm32-wasip1/release/rustpython_shim.wasm \
  ~/work/hanzo/base/plugins/extbench/fixtures/wazero-rustpython/rustpython.wasm
```

## Why deferred

The realistic build path requires:

1. `rustup target add wasm32-wasi` (host doesn't have rustup; only
   homebrew rust, which doesn't ship the wasi target).
2. Clone RustPython, configure Cargo.toml to expose the pointer ABI
   alongside the WASI command entrypoint, and rebuild.
3. Drop the resulting `rustpython.wasm` here.

Out of budget for this pass.

## Build path (when revisited)

```sh
# Install rustup + wasm32-wasi target (replaces homebrew rust).
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source "$HOME/.cargo/env"
rustup target add wasm32-wasi

# Build vanilla RustPython for wasm32-wasi.
cd /tmp
git clone --depth 1 --branch release-0.4 https://github.com/RustPython/RustPython
cd RustPython
cargo build --release --target wasm32-wasi --features freeze-stdlib
cp target/wasm32-wasi/release/rustpython.wasm \
  ~/work/hanzo/base/plugins/extbench/fixtures/wazero-rustpython/

# Then either:
# (a) Author a separate Rust shim crate that imports RustPython as a
#     dep, exports the pointer ABI, and dispatches to the embedded
#     interpreter (lower overhead, easier than CPython case).
# (b) Fork RustPython to add `validate(ptr, len)` directly alongside
#     its WASI command-style entrypoint.
```

## Expected numbers (informed prediction)

Compared to wazero-as (AssemblyScript, the fast wasm baseline):

- Throughput: ~10-30x slower per invoke. RustPython is a tree-
  walking interpreter (no JIT) running inside wasm; each Python
  bytecode op dispatches through a Rust match expression which is
  itself running on wasm linear memory.
- Cold start: ~10-50ms range. RustPython's startup compiles the
  validate.py module each time unless precompiled to a .pyc embedded
  in the wasm. With freeze-stdlib that's the stdlib only; user code
  still parses fresh.
- Per-module cost: ~5-10 MB compiled wasm + per-instance interpreter
  state (~10-20 MB). Lower than CPython-WASI.
- Hard sandbox: YES. Same crash-isolation win as wazero-as.

Decision rule for HIP-0105 / HIP-0106: pick wazero-rustpython over
pyvm if multi-tenant crash isolation is required AND the user
specifically wants Python-feel syntax. Starlark is a better answer
for "Python-feel DSL" — RustPython only wins when the user needs
actual Python semantics (classes, exceptions, dynamic typing) that
Starlark intentionally omits.

## When this fixture becomes runnable

1. Drop `rustpython.wasm` (or a shim wasm that re-exports the
   pointer ABI) into this directory.
2. The wasmvm runtime auto-picks it up at next `./run.sh`.
3. Update `docs/EXTENSIONS_BENCHMARK.md` "Full Cross-Runtime
   Comparison" section.
