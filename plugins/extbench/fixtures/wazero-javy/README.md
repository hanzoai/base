# wazero-javy fixture

Raw JS -> wasm via Shopify's [Javy](https://github.com/bytecodealliance/javy)
(QuickJS-as-wasm). Implements the shared `validate(payload)` contract.

## ABI note (important)

Javy emits a WASI **command** module: the guest reads its JSON input
from stdin and writes the JSON result to stdout via `_start`. Base's
`wasmvm` calling convention is pointer-based
(`validate(ptr, len) -> i64` packed result). The two ABIs do not match
out of the box. Two paths forward:

1. **`wazero_stdio` shim** — funnel the payload bytes through WASI's
   stdin/stdout fds and invoke `_start` per call. Re-entering `_start`
   on every call is what Javy is built for; the harness just needs an
   adapter Module that translates pointer-call to fd-call.
2. **Custom Javy plugin** — author a Rust Javy plugin that exports
   `validate(ptr, len) -> i64` directly. More work, lower per-invoke
   overhead.

Until the harness implements (1), this runtime reports as
`skipped (no matching guest ABI)` in the benchmark output. The
wazero-as fixture is the wasm representative for the headline numbers.

## Install Javy

Grab a v3+ release binary from
[github.com/bytecodealliance/javy/releases](https://github.com/bytecodealliance/javy/releases):

```sh
# macOS arm64 example — pick the binary matching your platform
curl -L -o javy.gz https://github.com/bytecodealliance/javy/releases/download/v3.0.0/javy-arm64-macos-v3.0.0.gz
gunzip javy.gz && chmod +x javy
sudo mv javy /usr/local/bin/javy
```

## Build

```sh
./build.sh
```

If `javy` is not on PATH the script exits non-zero. The committed
`validate.wasm` (if present) is the release artifact; absent until a
local Javy build runs.
