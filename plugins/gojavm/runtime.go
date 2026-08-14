// Package gojavm adapts zip's embedded JavaScript runtime
// (github.com/zap-proto/zip/js) to base's extruntime.Runtime SPI, so
// a manifest with `"runtime": "goja"` loads here. There is now exactly
// ONE goja engine in the Hanzo stack — zip's *js.JSRuntime — and
// base, cloud and every other zip consumer share it. gojavm is the thin
// extruntime projection of that engine: it owns manifest loading,
// TS/JSX/ESM bundling (esbuild, see transpile.go) and the JSON-bytes
// Invoke wire; the VM pool, host-fn registration and per-request VM
// isolation all live in zip/js.
//
// This is the lightweight JS option — pure Go, no cgo, shares the host
// heap. Use it when you don't need a hard sandbox.
//
// It sits alongside plugins/jsvm (the hook-style goja host for .base.js
// hook files). Extension directories with extension.json + runtime=goja
// go through here; hook files still go through jsvm. Collapsing jsvm onto
// zip/js as well is tracked separately — it needs base's host-API
// binds surface lifted into zip first.
package gojavm

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	zipjs "github.com/zap-proto/zip/js"

	"github.com/hanzoai/base/plugins/extruntime"
)

// defaultPoolSize is the number of pre-warmed goja VMs zip keeps hot.
// Override via BASE_GOJAVM_POOL_SIZE. Zero or negative selects zip's own
// default — every Invoke still borrows a VM, there is no per-call VM
// construction in the steady state.
const defaultPoolSize = 8

// poolSize is how many VMs the engine keeps warm, and therefore how many
// functions run at once — see [Run]. BASE_GOJAVM_POOL_SIZE overrides it.
func poolSize() int {
	if v := os.Getenv("BASE_GOJAVM_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultPoolSize
}

var (
	sharedOnce sync.Once
	sharedRT   *zipjs.JSRuntime
	sharedErr  error

	// admit holds one slot per warm VM. [Run] takes one for the length of a call.
	admit chan struct{}
)

// shared is the engine this process runs JS on. One pool, built once: the VMs it
// holds are what both the extension path and the function path borrow, so the
// number of goja VMs in a Base is a constant of the deployment rather than a
// function of its traffic.
func shared() (*zipjs.JSRuntime, error) {
	sharedOnce.Do(func() {
		size := poolSize()
		admit = make(chan struct{}, size)
		sharedRT, sharedErr = zipjs.NewJSRuntime(zipjs.JSOptions{
			PoolSize: size,
			HostFns:  map[string]any{dispatch: call},
		})
	})
	return sharedRT, sharedErr
}

// hosts is what a running call's token names. A call registers its host on the
// way in and drops it on the way out, so a token is an answer only while the
// call that owns it is still running.
type hosts struct {
	mu sync.RWMutex
	m  map[string]extruntime.Host
}

var live = &hosts{m: map[string]extruntime.Host{}}

func (t *hosts) put(tok string, h extruntime.Host) {
	t.mu.Lock()
	t.m[tok] = h
	t.mu.Unlock()
}

func (t *hosts) del(tok string) {
	t.mu.Lock()
	delete(t.m, tok)
	t.mu.Unlock()
}

func (t *hosts) get(tok string) extruntime.Host {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.m[tok]
}

// call is the one host global. It resolves the calling function's own host from
// its token, so what a name reaches is decided by whoever built that host and
// two calls running at once on two Bases never see each other's.
func call(tok, name, arg string) (string, error) {
	fn, ok := live.get(tok)[name]
	if !ok {
		return "", fmt.Errorf("gojavm: %q is not something this function may ask for", name)
	}

	out, err := fn([]byte(arg))
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "null", nil
	}
	return string(out), nil
}

// NewRuntime constructs a goja-backed extruntime.Runtime over [shared]. The
// engine is zip's *js.JSRuntime; require()/console/process are provided by zip's
// VM provisioning, so migrated TypeScript backends that `require(...)`,
// `console.log(...)` or read `process.env` run unchanged.
func NewRuntime() extruntime.Runtime {
	rt, err := shared()
	if err != nil {
		// NewJSRuntime only fails when a host fn fails to bind. Fall back to a
		// default runtime rather than panic at package use.
		rt, _ = zipjs.NewJSRuntime(zipjs.JSOptions{})
	}

	return &gojaRuntime{js: rt}
}

type gojaRuntime struct {
	js *zipjs.JSRuntime
}

func (*gojaRuntime) Name() string { return "goja" }

func (*gojaRuntime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"js"},
		HardSandbox:      false,
		Cgo:              false,
		// goja's Interrupt() is cooperative — guest code without
		// function-call opcodes (a tight for-loop on numerics) can
		// still resist it. Practically every real script yields
		// often enough that abort works, so we report true.
		SupportsAbort: true,
	}
}

func (r *gojaRuntime) Load(ctx context.Context, dir string) (extruntime.Module, error) {
	m, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Runtime != "goja" {
		return nil, fmt.Errorf("%w: goja runtime cannot load %q runtime", extruntime.ErrUnsupported, m.Runtime)
	}
	return newModule(r.js, dir, m)
}

func (r *gojaRuntime) Close() error {
	// zip's JSRuntime has no Close — its pooled VMs are plain values the
	// GC reclaims once the runtime is unreferenced. Drop the reference.
	r.js = nil
	return nil
}
