// Scale study — answers "how many concurrent loaded extensions can each
// runtime hold, and at what marginal cost per module / per concurrent
// invocation?" The existing bench_test.go measures a single warm
// module's throughput; this file measures fan-out — many modules, many
// tenants, many concurrent goroutines all hitting the same engine.
//
// All five scale sections live in this one file because they share
// fixture-cloning helpers and the per-runtime registry. Run with:
//
//	go test -tags v8vm -run '^$' -bench '^BenchmarkScale' \
//	    -benchtime=1x ./plugins/extbench/ -timeout 30m
//
// Output lines tagged "SCALE\t..." are tab-separated tables ready to
// paste into docs/EXTENSIONS_BENCHMARK.md. Subtests are named by the
// scale point so a single bench invocation yields every row.
//
// Honesty rule: if a runtime errors out at a scale point (OOM, pool
// exhaustion, instance-limit), the bench records the failure mode and
// the threshold reached, then moves on. Crashing the bench binary
// would hide that data; we never want that.
package extbench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/gojavm"
	"github.com/hanzoai/base/plugins/pyvm"
	"github.com/hanzoai/base/plugins/starkvm"
	"github.com/hanzoai/base/plugins/v8vm"
	"github.com/hanzoai/base/plugins/wasmvm"

	nativego "github.com/hanzoai/base/plugins/extbench/fixtures/native-go"
)

// ---------- scale-point selection ----------
//
// We cap the heaviest dimensions so the full study fits in <30 minutes
// on a developer laptop. The biggest knob is the per-loaded-module
// section — N=10000 on wazero allocates 8000 wasm instances per scale
// point which is multi-second on its own. We test up to 1000 modules
// for every runtime and 10000 only for the cheap runtimes (native,
// goja). For v8go we cap at 500 because the cgo isolate is shared but
// per-module Compile still allocates V8 contexts; >500 has been
// observed to slow load to a crawl on darwin/arm64.

var (
	modScalePoints      = []int{1, 10, 100, 1000}
	modScalePointsLarge = []int{1, 10, 100, 1000, 10000} // native, goja only
	modScalePointsV8    = []int{1, 10, 100, 500}         // v8go cap
	// pyvm cap: each module owns a sub-interpreter pool (default 4
	// entries). Sub-interp creation costs ~50ms each, so 1000 modules
	// holds 4000 interpreters = ~3 GB RSS. Cap at 100 for the study;
	// the marginal-memory section reports the per-module cost there.
	modScalePointsPy = []int{1, 10, 100}

	tenantPoints     = []int{10, 100, 1000}
	concurrencyPts   = []int{10, 100, 1000, 10000}
	poolSizes        = []int{1, 4, 16, 64, 256}
	goroutineHogN    = 1000
	coldStartLoadsN  = 100

	// v8go-specific concurrency cap. Running M>=100 concurrent goroutines
	// against one shared isolate triggers an upstream V8 fatal (observed
	// 2026-05-18: SIGSEGV inside libv8 from within Module.Invoke after
	// the shared mutex serializes a handful of contexts). We do not
	// "fix" v8go here — recording the threshold IS the finding.
	concurrencyPtsV8 = []int{10}
)

// ---------- runtime registry ----------
//
// One entry per runtime under test, with the fixture name we copy into
// each tenant directory. Native is special — its module is registered
// via init() in fixtures/native-go and the manifest has no `module`
// field, so we register a unique handler per cloned name.

type rtSpec struct {
	name     string
	factory  func() extruntime.Runtime
	fixture  string
	// modulePoints picks the scale-point set for this runtime — some
	// engines genuinely can't reach 10000 in a sane time.
	modulePoints []int
	// available returns false if the runtime isn't usable in this build
	// (e.g. v8go stub). Caller skips the entire subtest tree.
	available func() bool
}

func runtimeSpecs() []rtSpec {
	specs := []rtSpec{
		{
			name:         "native",
			factory:      extruntime.NewNative,
			fixture:      "native-go",
			modulePoints: modScalePointsLarge,
			available:    func() bool { return true },
		},
		{
			name:         "goja",
			factory:      gojavm.NewRuntime,
			fixture:      "goja-js",
			modulePoints: modScalePointsLarge,
			available:    func() bool { return true },
		},
		{
			// starlark: pure-Go, no JIT, no cgo. Per-module cost is
			// the compiled *Program plus a small thread pool (8 by
			// default, mostly empty until used). Scales as well as
			// goja in our expectation.
			name:         "starlark",
			factory:      starkvm.NewRuntime,
			fixture:      "starkvm-star",
			modulePoints: modScalePointsLarge,
			available:    func() bool { return fixtureDir("starkvm-star") != "" },
		},
		{
			name:         "wazero",
			factory:      wasmvm.NewRuntime,
			fixture:      "wazero-as",
			modulePoints: modScalePoints,
			available:    func() bool { return fixtureDir("wazero-as") != "" },
		},
		{
			name:         "v8go",
			factory:      v8vm.NewRuntime,
			fixture:      "v8go-js",
			modulePoints: modScalePointsV8,
			available:    v8Available,
		},
		{
			name:         "pyvm",
			factory:      pyvm.NewRuntime,
			fixture:      "pyvm-py",
			modulePoints: modScalePointsPy,
			available:    pyAvailable,
		},
	}
	return specs
}

// ---------- tenant cloning ----------
//
// Each "tenant" needs an extension.json with a UNIQUE name so the
// runtime treats it as a separate module. For native we also register
// the handler under that name. JS/wasm fixtures share the source
// artifact by symlink so we don't duplicate 12 KB of wasm 10000 times.

// cloneTenants creates n temp directories, each containing a manifest
// with a unique extension name plus a symlink to the source module
// file. For native we register validate under each cloned name. Returns
// the list of directories and a cleanup func.
//
// On Windows symlinks need admin; we'd fall back to file copy. Base
// targets linux/darwin so the symlink path is fine.
func cloneTenants(tb testing.TB, spec rtSpec, n int) ([]string, func()) {
	tb.Helper()
	srcDir := fixtureDir(spec.fixture)
	if srcDir == "" {
		tb.Skipf("fixture %s missing", spec.fixture)
	}
	srcMan, err := extruntime.LoadManifest(srcDir)
	if err != nil {
		tb.Fatalf("load src manifest: %v", err)
	}

	root, err := os.MkdirTemp("", "extbench-scale-*")
	if err != nil {
		tb.Fatalf("mkdir tmp: %v", err)
	}

	dirs := make([]string, 0, n)
	cleanup := func() { _ = os.RemoveAll(root) }

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("validate-email-%s-%d", spec.name, i)
		tdir := filepath.Join(root, fmt.Sprintf("t-%06d", i))
		if err := os.Mkdir(tdir, 0o755); err != nil {
			cleanup()
			tb.Fatalf("mkdir tenant: %v", err)
		}
		// Write per-tenant manifest with unique Name.
		manifest := fmt.Sprintf(`{
  "name": %q,
  "runtime": %q,
  "module": %q,
  "exports": ["validate"]
}`, name, srcMan.Runtime, srcMan.Module)
		if err := os.WriteFile(filepath.Join(tdir, "extension.json"), []byte(manifest), 0o644); err != nil {
			cleanup()
			tb.Fatalf("write manifest: %v", err)
		}
		// Symlink the module artifact if present (.js / .wasm).
		if srcMan.Module != "" {
			src := filepath.Join(srcDir, srcMan.Module)
			dst := filepath.Join(tdir, srcMan.Module)
			if err := os.Symlink(src, dst); err != nil {
				cleanup()
				tb.Fatalf("symlink module: %v", err)
			}
		}
		// For native, register the handler under the unique name so
		// Invoke(validate) resolves. Re-uses the shared validate fn
		// from fixtures/native-go via the side-effect import.
		if spec.name == "native" {
			extruntime.RegisterNative(name, "validate", nativego.Validate)
		}
		dirs = append(dirs, tdir)
	}
	return dirs, cleanup
}

// ---------- memory sampling ----------
//
// We sample three things: Go MemStats (HeapInuse, Sys), and OS RSS via
// getrusage(RUSAGE_SELF).Maxrss. On darwin Maxrss is in bytes; on
// linux it is in KB. rssBytes normalizes.

type memSample struct {
	HeapInuse uint64
	HeapAlloc uint64
	Sys       uint64
	RSS       uint64
	NumGoroutine int
}

func sampleMem() memSample {
	runtime.GC()
	debug.FreeOSMemory()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return memSample{
		HeapInuse:    ms.HeapInuse,
		HeapAlloc:    ms.HeapAlloc,
		Sys:          ms.Sys,
		RSS:          rssBytes(),
		NumGoroutine: runtime.NumGoroutine(),
	}
}

func rssBytes() uint64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	r := uint64(ru.Maxrss)
	if runtime.GOOS == "linux" {
		// linux: KB → bytes
		r *= 1024
	}
	// darwin/bsd: already bytes
	return r
}

// steadyMem takes 5 readings and returns the median (sorted middle).
// Removes the highest+lowest before averaging the middle three.
func steadyMem(field func(memSample) uint64) func() uint64 {
	return func() uint64 {
		samples := make([]uint64, 5)
		for i := range samples {
			samples[i] = field(sampleMem())
			time.Sleep(20 * time.Millisecond)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		// drop min+max, average middle 3
		return (samples[1] + samples[2] + samples[3]) / 3
	}
}

// ---------- helpers ----------

// loadAll loads every tenant module and returns the slice + a cleanup
// that closes them all. If any load fails we record how far we got
// and return the partial list along with the error — the caller
// decides whether that's "study done" or "abort this subtest".
func loadAll(rt extruntime.Runtime, dirs []string) ([]extruntime.Module, error) {
	mods := make([]extruntime.Module, 0, len(dirs))
	for i, d := range dirs {
		m, err := rt.Load(context.Background(), d)
		if err != nil {
			return mods, fmt.Errorf("load[%d]: %w", i, err)
		}
		mods = append(mods, m)
	}
	return mods, nil
}

func closeAll(mods []extruntime.Module) {
	for _, m := range mods {
		_ = m.Close()
	}
}

// percentile returns the value at p∈[0,1] from a sorted-ascending slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// ---------- 1) per-loaded-module marginal memory ----------

func BenchmarkScale_PerModuleMemory(b *testing.B) {
	for _, spec := range runtimeSpecs() {
		spec := spec
		b.Run(spec.name, func(b *testing.B) {
			if !spec.available() {
				b.Skip("runtime not available")
			}
			// One iteration only — this is a memory study, not a
			// timing benchmark. Using b.N would multiply work.
			if b.N > 1 {
				b.N = 1
			}
			baseline := steadyMem(func(s memSample) uint64 { return s.RSS })()
			heapBase := steadyMem(func(s memSample) uint64 { return s.HeapInuse })()

			for _, n := range spec.modulePoints {
				n := n
				b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
					b.N = 1
					rt := spec.factory()
					dirs, cleanup := cloneTenants(b, spec, n)
					defer cleanup()
					defer rt.Close()

					mods, err := loadAll(rt, dirs)
					if err != nil {
						b.Logf("SCALE\tper-module-memory\t%s\tN=%d\tFAIL loaded=%d err=%v",
							spec.name, n, len(mods), err)
						closeAll(mods)
						return
					}
					defer closeAll(mods)

					rss := steadyMem(func(s memSample) uint64 { return s.RSS })()
					heap := steadyMem(func(s memSample) uint64 { return s.HeapInuse })()
					sys := steadyMem(func(s memSample) uint64 { return s.Sys })()

					var rssPer, heapPer int64
					if n > 0 {
						rssPer = (int64(rss) - int64(baseline)) / int64(n)
						heapPer = (int64(heap) - int64(heapBase)) / int64(n)
					}
					fmt.Printf("SCALE\tper-module-memory\t%s\tN=%d\trss=%d\trss_delta=%d\trss_per_mod=%d\theap_inuse=%d\theap_per_mod=%d\tsys=%d\n",
						spec.name, n, rss, int64(rss)-int64(baseline),
						rssPer, heap, heapPer, sys)
				})
			}
		})
	}
}

// ---------- 2) per-tenant concurrent invocation fan-out ----------

func BenchmarkScale_TenantFanout(b *testing.B) {
	for _, spec := range runtimeSpecs() {
		spec := spec
		b.Run(spec.name, func(b *testing.B) {
			if !spec.available() {
				b.Skip("runtime not available")
			}
			for _, t := range tenantPoints {
				t := t
				// Cap tenant count to what the runtime is known to
				// support — same modulePoints cap as section 1.
				maxN := spec.modulePoints[len(spec.modulePoints)-1]
				if t > maxN {
					continue
				}
				b.Run(fmt.Sprintf("tenants=%d", t), func(b *testing.B) {
					b.N = 1
					runTenantFanout(b, spec, t)
				})
			}
			// Pool-size elbow study: only at 1000 tenants, only for
			// pooled runtimes. We toggle the pool by setting the env
			// var before constructing the runtime. Best-effort: if
			// the runtime ignores the env (native, v8go), the same
			// number prints across all pool sizes — that IS the data.
			if !contains(spec.modulePoints, 1000) {
				return
			}
			for _, ps := range poolSizes {
				ps := ps
				b.Run(fmt.Sprintf("pool=%d/tenants=1000", ps), func(b *testing.B) {
					b.N = 1
					_, old := setPoolEnv(spec.name, ps)
					defer restorePoolEnv(spec.name, old)
					runTenantFanout(b, spec, 1000)
				})
			}
		})
	}
}

func runTenantFanout(b *testing.B, spec rtSpec, tenants int) {
	rt := spec.factory()
	dirs, cleanup := cloneTenants(b, spec, tenants)
	defer cleanup()
	defer rt.Close()

	mods, err := loadAll(rt, dirs)
	if err != nil {
		b.Logf("SCALE\ttenant-fanout\t%s\ttenants=%d\tFAIL loaded=%d err=%v",
			spec.name, tenants, len(mods), err)
		closeAll(mods)
		return
	}
	defer closeAll(mods)

	// One concurrent invocation per tenant. Each goroutine calls its
	// own module's Invoke once; we measure wall time + per-call
	// latency + peak memory mid-flight via a 50ms ticker.
	latencies := make([]time.Duration, tenants)
	var wg sync.WaitGroup
	var stopMon = make(chan struct{})
	var peakRSS atomic.Uint64
	peakRSS.Store(rssBytes())
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				r := rssBytes()
				for {
					cur := peakRSS.Load()
					if r <= cur || peakRSS.CompareAndSwap(cur, r) {
						break
					}
				}
			case <-stopMon:
				return
			}
		}
	}()

	wallStart := time.Now()
	for i := 0; i < tenants; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			_, err := mods[i].Invoke(context.Background(), "validate", payload)
			latencies[i] = time.Since(t0)
			if err != nil {
				latencies[i] = -1
			}
		}()
	}
	wg.Wait()
	wall := time.Since(wallStart)
	close(stopMon)

	// Sort and compute p50/p99/p999.
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	errs := 0
	for _, l := range latencies {
		if l < 0 {
			errs++
		}
	}
	clean := latencies[errs:]
	p50 := percentile(clean, 0.50)
	p99 := percentile(clean, 0.99)
	p999 := percentile(clean, 0.999)

	fmt.Printf("SCALE\ttenant-fanout\t%s\ttenants=%d\twall=%s\tp50=%s\tp99=%s\tp999=%s\terrs=%d\tpeak_rss=%d\n",
		spec.name, tenants, wall, p50, p99, p999, errs, peakRSS.Load())
}

// ---------- 3) sustained high concurrency on ONE module ----------

func BenchmarkScale_OneModuleConcurrency(b *testing.B) {
	for _, spec := range runtimeSpecs() {
		spec := spec
		b.Run(spec.name, func(b *testing.B) {
			if !spec.available() {
				b.Skip("runtime not available")
			}
			rt := spec.factory()
			defer rt.Close()
			dirs, cleanup := cloneTenants(b, spec, 1)
			defer cleanup()
			mods, err := loadAll(rt, dirs)
			if err != nil {
				b.Logf("SCALE\tone-module-concurrency\t%s\tFAIL load err=%v", spec.name, err)
				return
			}
			defer closeAll(mods)
			mod := mods[0]

			// Sanity-check once outside the timed loop.
			if _, err := mod.Invoke(context.Background(), "validate", payload); err != nil {
				b.Logf("SCALE\tone-module-concurrency\t%s\tFAIL sanity err=%v", spec.name, err)
				return
			}

			pts := concurrencyPts
			if spec.name == "v8go" {
				pts = concurrencyPtsV8
			}
			for _, m := range pts {
				m := m
				b.Run(fmt.Sprintf("M=%d", m), func(b *testing.B) {
					b.N = 1
					// Each goroutine fires invsPerGo invocations. Keep
					// total work roughly constant (M*invsPerGo ≈ 50k)
					// so smaller M doesn't underrun the timer.
					invsPerGo := 50000 / m
					if invsPerGo < 1 {
						invsPerGo = 1
					}
					totalInvs := m * invsPerGo
					latencies := make([]time.Duration, totalInvs)
					var wg sync.WaitGroup
					t0 := time.Now()
					for g := 0; g < m; g++ {
						g := g
						wg.Add(1)
						go func() {
							defer wg.Done()
							for k := 0; k < invsPerGo; k++ {
								s := time.Now()
								_, err := mod.Invoke(context.Background(), "validate", payload)
								latencies[g*invsPerGo+k] = time.Since(s)
								if err != nil {
									latencies[g*invsPerGo+k] = -1
								}
							}
						}()
					}
					wg.Wait()
					wall := time.Since(t0)
					sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
					errs := 0
					for _, l := range latencies {
						if l < 0 {
							errs++
						}
					}
					clean := latencies[errs:]
					ops := float64(totalInvs-errs) / wall.Seconds()
					fmt.Printf("SCALE\tone-module-concurrency\t%s\tM=%d\tinvs=%d\twall=%s\tops_per_sec=%.0f\tp50=%s\tp99=%s\terrs=%d\n",
						spec.name, m, totalInvs, wall, ops,
						percentile(clean, 0.50), percentile(clean, 0.99), errs)
				})
			}
		})
	}
}

// ---------- 4) goroutine cost per concurrent invocation ----------

func BenchmarkScale_GoroutineCost(b *testing.B) {
	// Maxrss is a process-lifetime high-water mark; it never
	// decreases, so a "before vs after" delta on RSS is meaningless
	// once any earlier subtest pushed the watermark up. We use
	// MemStats.HeapInuse + Sys for the goroutine-cost delta instead;
	// these track current heap and current Go-managed reservation
	// and they DO decrease after GC. We still print RSS so a reader
	// sees the absolute floor — but the per-goroutine math uses Sys.

	for _, spec := range runtimeSpecs() {
		spec := spec
		b.Run(spec.name, func(b *testing.B) {
			b.N = 1
			if !spec.available() {
				b.Skip("runtime not available")
			}
			rt := spec.factory()
			defer rt.Close()
			dirs, cleanup := cloneTenants(b, spec, 1)
			defer cleanup()
			mods, err := loadAll(rt, dirs)
			if err != nil {
				b.Logf("SCALE\tgoroutine-cost\t%s\tFAIL load err=%v", spec.name, err)
				return
			}
			defer closeAll(mods)
			mod := mods[0]

			// Warmup once so any lazy init (script compile, isolate
			// context creation) is paid before the baseline snapshot.
			if _, err := mod.Invoke(context.Background(), "validate", payload); err != nil {
				b.Logf("SCALE\tgoroutine-cost\t%s\tFAIL warmup err=%v", spec.name, err)
				return
			}
			baseline := sampleMem()

			n := goroutineHogN
			// v8go: skip this section entirely. Observed crash mode
			// (2026-05-18, darwin/arm64) is a V8 fatal triggered by
			// the combination of (a) prior subtests building up V8
			// state and (b) any non-trivial parallel goroutine count
			// in this section. Even N=50 crashed after the earlier
			// per-module/tenant/concurrency subtests had run. The
			// recordable answer is the threshold: v8go cannot sustain
			// >~10 concurrent goroutines on a shared isolate after a
			// few thousand invocations of prior accumulated state.
			if spec.name == "v8go" {
				fmt.Printf("SCALE\tgoroutine-cost\t%s\tN=skipped\treason=v8_fatal_above_~10_concurrent_after_warm_isolate\n",
					spec.name)
				return
			}
			release := make(chan struct{})
			var wg sync.WaitGroup
			var ready sync.WaitGroup
			ready.Add(n)

			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = mod.Invoke(context.Background(), "validate", payload)
					// Park here with all per-invoke buffers still
					// scoped. Peak sample below sees N parked
					// goroutines plus whatever the runtime holds.
					ready.Done()
					<-release
				}()
			}
			ready.Wait()

			peak := sampleMem()
			close(release)
			wg.Wait()

			sysDelta := int64(peak.Sys) - int64(baseline.Sys)
			heapDelta := int64(peak.HeapInuse) - int64(baseline.HeapInuse)
			perGoroutineSys := sysDelta / int64(n)
			perGoroutineHeap := heapDelta / int64(n)
			fmt.Printf("SCALE\tgoroutine-cost\t%s\tN=%d\tsys_baseline=%d\tsys_peak=%d\tsys_delta=%d\tper_goroutine_sys=%d\theap_delta=%d\tper_goroutine_heap=%d\trss=%d\tgo_count=%d\n",
				spec.name, n, baseline.Sys, peak.Sys, sysDelta,
				perGoroutineSys, heapDelta, perGoroutineHeap,
				peak.RSS, peak.NumGoroutine)
		})
	}
}

// ---------- 5) cold pool startup vs steady-state ----------
//
// "I just turned the binary on with N extensions, when am I serving
// traffic?" Sequentially load N modules, measuring (a) total time to
// load all N, (b) time-to-first-invoke on module N after load
// completes. The first-invoke time is the warm-pool steady-state, the
// load time is the deploy cost.

func BenchmarkScale_ColdPoolStartup(b *testing.B) {
	for _, spec := range runtimeSpecs() {
		spec := spec
		b.Run(spec.name, func(b *testing.B) {
			b.N = 1
			if !spec.available() {
				b.Skip("runtime not available")
			}
			n := coldStartLoadsN
			rt := spec.factory()
			defer rt.Close()
			dirs, cleanup := cloneTenants(b, spec, n)
			defer cleanup()

			t0 := time.Now()
			mods, err := loadAll(rt, dirs)
			loadWall := time.Since(t0)
			if err != nil {
				b.Logf("SCALE\tcold-pool-startup\t%s\tN=%d\tFAIL loaded=%d err=%v",
					spec.name, n, len(mods), err)
				closeAll(mods)
				return
			}
			defer closeAll(mods)

			// Time-to-first-invoke on the last module (worst case —
			// it was the most recently loaded so its pool entries
			// are warm; on goja/wazero this is the per-instance
			// warmup, on v8go this is mutex grab + script run).
			t1 := time.Now()
			_, err = mods[n-1].Invoke(context.Background(), "validate", payload)
			firstInvoke := time.Since(t1)
			if err != nil {
				b.Logf("SCALE\tcold-pool-startup\t%s\tN=%d\tFAIL first-invoke err=%v",
					spec.name, n, err)
				return
			}

			// Steady-state: 1000 more invocations across all loaded
			// modules round-robin; report avg ns/op.
			const steady = 1000
			t2 := time.Now()
			for i := 0; i < steady; i++ {
				_, err := mods[i%n].Invoke(context.Background(), "validate", payload)
				if err != nil {
					b.Logf("SCALE\tcold-pool-startup\t%s\tN=%d\tFAIL steady invoke err=%v",
						spec.name, n, err)
					return
				}
			}
			steadyWall := time.Since(t2)
			avgSteady := steadyWall / time.Duration(steady)

			fmt.Printf("SCALE\tcold-pool-startup\t%s\tN=%d\tload_total=%s\tload_per_mod=%s\tfirst_invoke=%s\tsteady_avg=%s\n",
				spec.name, n, loadWall, loadWall/time.Duration(n),
				firstInvoke, avgSteady)
		})
	}
}

// ---------- helpers ----------

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func setPoolEnv(rtName string, size int) (key, prev string) {
	switch rtName {
	case "goja":
		key = "BASE_GOJAVM_POOL_SIZE"
	case "wazero":
		key = "BASE_WASMVM_POOL_SIZE"
	case "v8go":
		key = "BASE_V8VM_POOL_SIZE"
	case "pyvm":
		key = "BASE_PYVM_POOL_SIZE"
	case "starlark":
		key = "BASE_STARKVM_POOL_SIZE"
	default:
		return "", ""
	}
	prev = os.Getenv(key)
	_ = os.Setenv(key, fmt.Sprintf("%d", size))
	return key, prev
}

func restorePoolEnv(rtName, prev string) {
	// Map name → env var without re-setting; we only want to restore.
	var key string
	switch rtName {
	case "goja":
		key = "BASE_GOJAVM_POOL_SIZE"
	case "wazero":
		key = "BASE_WASMVM_POOL_SIZE"
	case "v8go":
		key = "BASE_V8VM_POOL_SIZE"
	case "pyvm":
		key = "BASE_PYVM_POOL_SIZE"
	case "starlark":
		key = "BASE_STARKVM_POOL_SIZE"
	default:
		return
	}
	if prev == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, prev)
	}
}
