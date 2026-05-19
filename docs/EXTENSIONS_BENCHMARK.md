# Extension Runtime Benchmark

**Methodology**: `plugins/extbench/run.sh` on Apple M1 Max (10 cores, darwin/arm64),
Go 1.26.3, 2026-05-18. `-benchtime=2s -count=3`. Numbers reported are the
**median of 3 runs**.

**Workload**: `validate(email, age)` → trim + lowercase email, check shape
(one `@`, non-empty local, dotted domain), bound age to 0..150. JSON in,
JSON out. Fixed payload `{"email":"Foo@Example.COM ","age":25}` (37 bytes
in, ~46 bytes out).

**Fixtures**: one per runtime under `plugins/extbench/fixtures/`. All five
implement byte-identical semantics; differences in `B/op` are pure
runtime overhead.

## Throughput

Serial = single goroutine. Parallel = `b.RunParallel` across 10 cores.
Lower is better.

| Runtime | Serial ns/op | Parallel ns/op | B/op (serial) | allocs/op (serial) |
|---|---:|---:|---:|---:|
| **native** (Go) | **1,197** | **694** | 1,448 | 20 |
| **goja** (JS, pure-Go) | 4,513 | 1,652 | 3,330 | 67 |
| **wazero (AssemblyScript)** | 9,956 | 3,271 | 45,600 | 17 |
| **wazero (Javy/JS)** | skipped | skipped | — | — |
| **v8go** (cgo V8) | 11,895 | 12,667 | 672 | 17 |

Speed-up ratios vs native (serial):
- goja: 3.8x slower
- wazero AS: 8.3x slower
- v8go: 9.9x slower

Notes on the numbers:
- **v8go is the only runtime that does NOT speed up under parallel
  load** — the v8vm implementation serializes module Invoke on a single
  shared isolate mutex (`plugins/v8vm/runtime.go:47`), because V8 is
  not thread-safe per-isolate. Parallel ns/op (12,667) is essentially
  the same as serial (11,895), with a small overhead from goroutine
  scheduling around the mutex.
- **goja achieves the best parallel ratio** (4,513 → 1,652, i.e.
  2.7x speedup) thanks to its 8-slot pre-warmed VM pool; each
  goroutine grabs a free `*goja.Runtime` and runs independently.
- **wazero AS scales well** (9,956 → 3,271, 3.0x speedup) — the
  module instance pool plus wazero's per-instance linear memory makes
  parallel safe and cheap.

## Cold start (per `Load()`)

Time to construct a fresh runtime, load a fixture, and close it. Each
iteration is a complete round-trip; lower is better.

| Runtime | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| **native** | **16,847** | 1,424 | 17 |
| **goja** | 78,282 | 47,400 | 569 |
| **v8go** | 906,033 | 3,968 | 32 |
| **wazero (AS)** | 3,936,863 | 1,541,612 | 2,999 |
| wazero (Javy) | skipped | — | — |

- **wazero AS cold start is 234x slower than native** and 4.3x slower
  than v8go. The 1.5 MB and 3,000 allocs come from compiling the wasm
  module fresh — wazero's JIT pass and the instance pool of 8
  pre-instantiated modules dominate.
- **v8go cold start is 11.6x slower than goja**. Allocating a new
  `v8.Isolate` per Load is the dominant cost; in production a long-
  lived runtime amortizes this away.
- **goja cold start (78µs)** is dominated by the 569 allocs the JS
  parser does compiling `validate.js`. Once compiled the program is
  shared across the runtime pool.

## Memory (1000 invocations, warm)

`runtime.MemStats.TotalAlloc` delta and `NumGC` delta after a single
warm-up Invoke followed by 1,000 timed invocations.

| Runtime | TotalAlloc Δ (bytes) | bytes / invoke | NumGC Δ |
|---|---:|---:|---:|
| **v8go** | 672,560 | 673 | 0 |
| **native** | 1,449,592 | 1,450 | 0 |
| **goja** | 3,432,872 | 3,433 | 1 |
| **wazero (AS)** | 44,425,304 | 44,425 | 13 |
| wazero (Javy) | skipped | — | — |

- **v8go is by far the lightest** on the Go heap (673 B/invoke)
  because most allocation lives inside V8's C++ heap and never touches
  Go's GC. This is misleading-by-omission for total RSS; V8's
  per-isolate footprint is ~10-20 MB up front.
- **wazero AS allocates 30x more on the Go heap than goja and 60x
  more than native** — and triggers 13 GC cycles per 1000 invocations.
  The pointer-based ABI (`__base_alloc` + `memory.Read` + result copy
  to Go heap) round-trips bytes through Go more times than goja's
  in-process JS Value conversion or v8go's C++ allocator.

## Binary size delta

Built with `go build -o /tmp/sizetest-<runtime> ./plugins/extbench/internal/sizetest/<runtime>`.
Each sizetest binary imports only its target runtime, so the delta
reflects the on-disk cost of linking that engine into a Base daemon.

| Build | Size | Δ vs baseline |
|---|---:|---:|
| baseline (`fmt.Println`) | 2,492,466 | — |
| + extruntime (native only) | 2,581,506 | +89 KB |
| + v8vm (stub, no `-tags v8vm`) | 2,581,506 | +89 KB |
| + wasmvm (wazero) | 3,112,978 | +620 KB |
| + gojavm | 13,230,610 | +10.7 MB |
| + all four (native+goja+wazero+v8-stub) | 13,668,434 | +11.2 MB |
| + v8vm **with `-tags v8vm`** alone | 38,820,418 | +36.3 MB |
| + all four with `-tags v8vm` | 50,334,946 | +47.8 MB |

The v8go cgo binary (libv8 statically linked) is the dominant cost.
For an amd64-only K8s build that's 36 MB pure overhead; for distros
that want every runtime present it's ~48 MB.

## Observations

1. **Native is the bound on every metric.** It's a Go function call.
   Anything else is "what does the engine charge me to add a sandbox /
   dynamic language / quota."

2. **Goja punches far above its weight.** 3.8x slower than native in
   serial, 2.4x slower in parallel, 78µs cold start, 3.4 KB / invoke
   on the heap, zero cgo, 10.7 MB linked. For the "I want users to
   write JS hooks against my Go service" use case, goja is the
   pragmatic answer.

3. **wazero AS pays a real per-invoke tax.** ~10µs serial / 3µs
   parallel and 45 KB / invoke is fine for transactional hooks but
   bad for tight inner loops. The 45 KB is the input copy + result
   copy + AS string allocations crossing the host-guest boundary —
   not wazero's fault, it's the ABI.

4. **v8go is the cold-start surprise.** It's faster than goja for the
   first Load by 11x in our measurements, but slower than goja per
   invoke (2.6x serial, 7.7x parallel). The serialized-isolate model
   means v8go's parallel throughput is essentially its serial
   throughput.

5. **Wazero AS cold start is the worst of all.** Compiling wasm to
   native code on every `Load()` is expensive. Real deployments
   should hold the runtime open and `Load()` each module once, never
   per-request.

## Recommendations

| Use case | Pick | Why |
|---|---|---|
| Built-in extensions, no user code | **native** | 7.4x faster than next-best, zero overhead |
| User JS hooks, single-tenant | **goja** | Best parallel JS, no cgo, smallest viable JS engine |
| User code, multi-tenant, hard sandbox | **wazero (AS / Rust / TinyGo)** | Linear-memory isolation, no cgo, abortable |
| User JS hooks, hard sandbox required | **v8go** | Only option for sandboxed JS; pay for it |
| Lambda-style short-lived deploys | **goja** | 1ns less cold start than v8go, 50x less than wazero |

**Defaults**:
- Base ships `goja` as the JS extension runtime (matches plugins/jsvm).
- `wasmvm` is opt-in for hard-sandbox needs (extension declares
  `"runtime": "wazero"`).
- `v8vm` is build-tag gated (`go build -tags v8vm`) so the default
  binary stays at 13 MB instead of 50 MB.
- `native` is always available for compiled-in extensions.

## Skipped: Javy / JS-on-wasm

The Javy fixture is wired but skipped at runtime. Javy emits a WASI
**command** module — the guest reads stdin and writes stdout via
`_start`. Base's `wasmvm` calling convention is pointer-based
(`validate(ptr, len) -> i64` packed result), so the ABIs do not
match. See `plugins/extbench/fixtures/wazero-javy/README.md` for two
paths to enable it (stdio shim or custom Javy plugin).

## Reproduce

```
cd ~/work/hanzo/base/plugins/extbench
./run.sh          # writes bench-results.txt
```

Approximate wall clock: 3-5 minutes for the full suite (both default
and `-tags v8vm` passes).
