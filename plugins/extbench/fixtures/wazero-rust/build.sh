#!/usr/bin/env bash
# Build the Rust wasm fixture. The committed validate.wasm is what the
# bench harness loads; re-run this only if src/lib.rs changes.
#
# wasm32-unknown-unknown (no WASI), opt-level=z + lto for smallest output.

set -euo pipefail
cd "$(dirname "$0")"

# Source rustup env so we pick up `wasm32-unknown-unknown` even when the
# caller's PATH only has homebrew rust (homebrew rust does not ship the
# wasm32 std crates).
if [ -f "$HOME/.cargo/env" ]; then
  # shellcheck disable=SC1091
  . "$HOME/.cargo/env"
fi
# Ensure rustup-managed rustc takes precedence over a brew install (brew
# ships only the host stdlib, no wasm32-unknown-unknown).
export PATH="$HOME/.cargo/bin:$PATH"

cargo build --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/validate_rust.wasm ./validate.wasm
ls -la validate.wasm
