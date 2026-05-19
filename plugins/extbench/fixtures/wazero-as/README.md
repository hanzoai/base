# wazero-as fixture

AssemblyScript -> wasm fixture for the Base extension benchmark harness.
Implements the shared `validate(payload)` contract over the Base wasmvm
host-guest ABI:

```
__base_alloc(size: i32) -> i32
__base_free(ptr: i32, size: i32)
validate(ptr: i32, len: i32) -> i64   // high32=resultPtr, low32=resultLen
```

## Build

```sh
npm install
npm run asbuild
```

The committed `validate.wasm` is the release artifact. Re-build only if
you change `src/assembly/index.ts`.

## Verify exports

```sh
wasm-objdump -x validate.wasm | grep -E 'validate|__base_alloc|__base_free'
```
