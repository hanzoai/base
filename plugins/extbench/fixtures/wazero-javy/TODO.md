# TODO(extbench): build validate.wasm with javy

Javy was not present on the build host when this fixture was authored
(`which javy` returned non-zero). Two follow-ups:

1. Install Javy v3+ (see README.md) and run `./build.sh` to produce
   `validate.wasm`, then commit it.
2. Land the `wazero_stdio` shim in `plugins/wasmvm/` so the harness can
   actually invoke a Javy command module (see README.md "ABI note").
   Without the shim the fixture's wasm is not consumable by the
   existing wasmvm loader; the bench harness should skip this runtime
   with `skipped (no matching guest ABI)` until the shim lands.
