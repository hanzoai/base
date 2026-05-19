// Package gojavm wraps the dop251/goja JavaScript engine as an
// extruntime.Runtime, so a manifest with `"runtime": "goja"` loads
// here. This is the lightweight JS option — pure Go, no cgo, shares
// the host heap. Use it when you don't need a hard sandbox.
//
// This is alongside plugins/jsvm (the hook-style goja host), not a
// replacement. Hook files (.base.js) still go through jsvm. Extension
// directories with extension.json + runtime=goja go through here.
package gojavm

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/dop251/goja"
	"github.com/hanzoai/base/plugins/extruntime"
)

// defaultPoolSize is the number of pre-warmed goja runtimes kept in the
// pool. Override via BASE_GOJAVM_POOL_SIZE. Zero or negative disables
// pre-warming — every Invoke creates a fresh runtime.
const defaultPoolSize = 8

// NewRuntime constructs a goja-backed extruntime.Runtime. Each module
// loaded by the returned runtime owns its own program (compiled JS)
// and reuses runtimes from a process-wide pool.
func NewRuntime() extruntime.Runtime {
	size := defaultPoolSize
	if v := os.Getenv("BASE_GOJAVM_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = n
		}
	}
	return &gojaRuntime{pool: newPool(size, func() *goja.Runtime { return goja.New() })}
}

type gojaRuntime struct {
	pool *vmsPool
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
	return newModule(r.pool, dir, m)
}

func (r *gojaRuntime) Close() error {
	// Pool entries are plain goja.Runtime values — no Close on goja
	// itself. Drop the slice so the GC can reclaim.
	r.pool.mux.Lock()
	r.pool.items = nil
	r.pool.mux.Unlock()
	return nil
}

// ---------- pool ----------
//
// This mirrors plugins/jsvm/pool.go exactly so behavior is identical
// (item-level mutex, busy flag, factory fallback when fully saturated).
// We keep a copy here rather than importing jsvm to avoid pulling in
// the entire hook/migration/binding surface for an extension-loader
// use case. One pool implementation per package, both tiny.

type poolItem struct {
	mux  sync.Mutex
	busy bool
	vm   *goja.Runtime
}

type vmsPool struct {
	mux     sync.RWMutex
	factory func() *goja.Runtime
	items   []*poolItem
}

func newPool(size int, factory func() *goja.Runtime) *vmsPool {
	p := &vmsPool{factory: factory}
	if size > 0 {
		p.items = make([]*poolItem, size)
		for i := 0; i < size; i++ {
			p.items[i] = &poolItem{vm: factory()}
		}
	}
	return p
}

// run executes call with a pooled vm; if every pool slot is busy it
// constructs a one-off vm and discards it after the call returns.
func (p *vmsPool) run(call func(vm *goja.Runtime) error) error {
	p.mux.RLock()

	var freeItem *poolItem
	for _, item := range p.items {
		item.mux.Lock()
		if item.busy {
			item.mux.Unlock()
			continue
		}
		item.busy = true
		item.mux.Unlock()
		freeItem = item
		break
	}

	p.mux.RUnlock()

	if freeItem == nil {
		return call(p.factory())
	}

	err := call(freeItem.vm)

	freeItem.mux.Lock()
	freeItem.busy = false
	freeItem.mux.Unlock()

	return err
}
