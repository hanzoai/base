#!/usr/bin/env bash
# Build validate.wasm via Shopify's Javy (QuickJS -> wasm).
#
# Install Javy v3+: https://github.com/bytecodealliance/javy/releases
#   curl -L <release-url> -o javy.gz && gunzip javy.gz && chmod +x javy
#   sudo mv javy /usr/local/bin/javy
set -euo pipefail

cd "$(dirname "$0")"

if ! command -v javy >/dev/null 2>&1; then
  echo "javy not found on PATH. Install from https://github.com/bytecodealliance/javy/releases (v3+)." >&2
  exit 1
fi

javy build validate.js -o validate.wasm
ls -la validate.wasm
