# wazero-rust fixture

Pure-Rust `validate(email, age)` compiled to `wasm32-unknown-unknown`,
loaded by the Base wasmvm runtime. Implements the shared HIP-0105
pointer/length ABI directly (no zip-rs / no Hanzo-internal deps):

```
__base_alloc(size: i32) -> i32
__base_free(ptr: i32, size: i32)
validate(ptr: i32, len: i32) -> i64   // high32=resultPtr, low32=resultLen
```

## Build

```sh
./build.sh
```

The committed `validate.wasm` is the release artifact. Re-build only if
you change `src/lib.rs`.

Requires the `wasm32-unknown-unknown` Rust target — install with
`rustup target add wasm32-unknown-unknown` if missing (homebrew rust
does not ship the wasm32 std crates).

## Why not `wasm32-wasip1`?

This fixture imports zero WASI symbols. Building against
`wasm32-unknown-unknown` produces a smaller artifact (no WASI imports
to satisfy) and avoids accidentally pulling in `std` features that
need a real OS. The Base wasmvm host still instantiates WASI globally,
so modules that *do* import WASI work too — the existing
`wazero-as` fixture is one example.

## Verify exports

```sh
wasm-objdump -x validate.wasm | grep -E 'validate|__base_alloc|__base_free'
```
