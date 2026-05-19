package wasmvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Host-guest ABI symbol names. Guests must export these for Invoke to
// have a place to write the payload and read the result. Keep them
// short so manual `wasm-objdump -x` is readable.
const (
	allocFn = "__base_alloc"
	freeFn  = "__base_free"
)

// poolDefault is small on purpose. Wasm modules amortize compilation
// once but each instance owns its linear memory; eight is enough to
// soak typical bursty hook traffic without ballooning RSS.
const poolDefault = 8

// poolEnv lets ops override the per-module instance pool size without
// recompiling. Anything <=0 or unparseable falls back to poolDefault.
const poolEnv = "BASE_WASMVM_POOL_SIZE"

// wasmModule wraps a single compiled wasm artifact plus a fixed pool of
// instantiated modules. Each Invoke checks out an instance, does its
// call, and returns it — re-instantiation is the dominant cost we want
// to avoid per invocation.
type wasmModule struct {
	name     string
	exports  []string
	compiled wazero.CompiledModule
	rt       wazero.Runtime

	// pool holds ready-to-use module instances. Buffered so checkout
	// is a non-blocking happy-path under load.
	pool chan api.Module

	// seq monotonically names new instances so post-cancel refills
	// don't collide with wazero's module namespace.
	seq atomic.Uint64

	mu     sync.Mutex
	closed bool
}

func loadModule(ctx context.Context, rt wazero.Runtime, dir string) (extruntime.Module, error) {
	man, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if man.Runtime != "wazero" {
		return nil, fmt.Errorf("%w: wazero runtime cannot load %q runtime", extruntime.ErrUnsupported, man.Runtime)
	}
	if man.Module == "" {
		return nil, fmt.Errorf("%w: module path is required", extruntime.ErrBadManifest)
	}

	wasmPath := filepath.Join(dir, man.Module)
	bin, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("wasmvm: read %s: %w", wasmPath, err)
	}

	compiled, err := rt.CompileModule(ctx, bin)
	if err != nil {
		return nil, fmt.Errorf("wasmvm: compile %s: %w", wasmPath, err)
	}

	size := poolSize()
	m := &wasmModule{
		name:     man.Name,
		exports:  man.Exports,
		compiled: compiled,
		rt:       rt,
		pool:     make(chan api.Module, size),
	}

	// Pre-instantiate the pool. Failing here surfaces ABI errors at
	// Load time instead of at first Invoke.
	for i := 0; i < size; i++ {
		inst, err := m.instantiate(ctx)
		if err != nil {
			m.Close()
			return nil, err
		}
		m.pool <- inst
	}
	return m, nil
}

func (m *wasmModule) instantiate(ctx context.Context) (api.Module, error) {
	// Each instance gets a unique monotonic name so wazero doesn't
	// collide them in its module namespace (refills after cancel
	// would otherwise reuse a destroyed name).
	id := m.seq.Add(1)
	// Default WithRandSource is crypto/rand which is what we want;
	// don't override. Stdout/stderr go to the host process so guest
	// `console.log` / `eprintln!` is visible in logs.
	cfg := wazero.NewModuleConfig().
		WithName(fmt.Sprintf("%s#%d", m.name, id)).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithSysWalltime().
		WithSysNanotime()
	inst, err := m.rt.InstantiateModule(ctx, m.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("wasmvm: instantiate %s: %w", m.name, err)
	}
	return inst, nil
}

func (m *wasmModule) Name() string      { return m.name }
func (m *wasmModule) Runtime() string   { return "wazero" }
func (m *wasmModule) Exports() []string { return m.exports }

func (m *wasmModule) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, extruntime.ErrClosed
	}

	// Checkout — respect ctx so a cancelled caller doesn't wait on a
	// full pool forever.
	var inst api.Module
	select {
	case inst = <-m.pool:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// replace is set when the instance was hard-aborted (ctx cancel)
	// and a fresh one must be instantiated to keep the pool size stable.
	var replace bool
	defer func() {
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()

		if replace {
			// Pool entry was destroyed mid-call; refill from a fresh
			// instance using a background ctx so cancel of the caller
			// doesn't bleed into instantiation.
			if !closed {
				if fresh, err := m.instantiate(context.Background()); err == nil {
					m.pool <- fresh
				}
			}
			return
		}
		if inst == nil {
			return
		}
		if closed {
			_ = inst.Close(context.Background())
			return
		}
		m.pool <- inst
	}()

	exp := inst.ExportedFunction(fn)
	if exp == nil {
		return nil, fmt.Errorf("%w: %s:%s", extruntime.ErrUnknownFn, m.name, fn)
	}
	alloc := inst.ExportedFunction(allocFn)
	free := inst.ExportedFunction(freeFn)
	if alloc == nil || free == nil {
		return nil, fmt.Errorf("%w: %s missing %s/%s exports", extruntime.ErrUnsupported, m.name, allocFn, freeFn)
	}

	// Watch ctx — on cancel, hard-abort the module. wazero's Close
	// flips an atomic the engine checks between instructions, so an
	// in-flight Call returns an error promptly.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = inst.Close(context.Background())
		case <-done:
		}
	}()

	// Write payload into guest memory.
	plen := uint64(len(payload))
	allocRes, err := alloc.Call(ctx, plen)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			replace = true
			return nil, ctxErr
		}
		return nil, fmt.Errorf("wasmvm: alloc: %w", err)
	}
	pptr := uint32(allocRes[0])
	if !inst.Memory().Write(pptr, payload) {
		return nil, fmt.Errorf("wasmvm: payload write out of bounds (ptr=%d len=%d)", pptr, plen)
	}

	// Call fn(ptr, len) -> i64 packed (resPtr<<32 | resLen).
	out, err := exp.Call(ctx, uint64(pptr), plen)
	// Always free the input buffer; guest doesn't own it.
	_, _ = free.Call(ctx, uint64(pptr), plen)
	if err != nil {
		// Distinguish ctx cancel — wazero wraps the engine error; the
		// caller cares whether their cancel landed or the guest blew up.
		if ctxErr := ctx.Err(); ctxErr != nil {
			replace = true
			return nil, ctxErr
		}
		return nil, fmt.Errorf("wasmvm: call %s: %w", fn, err)
	}
	if len(out) != 1 {
		return nil, fmt.Errorf("wasmvm: %s returned %d values, want 1 (packed i64)", fn, len(out))
	}

	packed := out[0]
	rptr := uint32(packed >> 32)
	rlen := uint32(packed & 0xFFFFFFFF)
	if rlen == 0 {
		return []byte{}, nil
	}

	buf, ok := inst.Memory().Read(rptr, rlen)
	if !ok {
		return nil, fmt.Errorf("wasmvm: result read out of bounds (ptr=%d len=%d)", rptr, rlen)
	}
	// Copy out before freeing — guest memory may be reused next call.
	result := make([]byte, rlen)
	copy(result, buf)
	_, _ = free.Call(ctx, uint64(rptr), uint64(rlen))

	return result, nil
}

func (m *wasmModule) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	// Drain best-effort without closing the channel — concurrent Invoke
	// goroutines may still be in flight and would panic on a send to a
	// closed channel. Non-blocking receive gives us every instance
	// currently checked in; instances mid-flight will hit the closed
	// flag on their next Invoke and skip return-to-pool.
	var firstErr error
drain:
	for {
		select {
		case inst := <-m.pool:
			if err := inst.Close(context.Background()); err != nil && firstErr == nil {
				firstErr = err
			}
		default:
			break drain
		}
	}
	if err := m.compiled.Close(context.Background()); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// poolSize reads BASE_WASMVM_POOL_SIZE, falling back to the default
// for unset / invalid / non-positive values.
func poolSize() int {
	v := os.Getenv(poolEnv)
	if v == "" {
		return poolDefault
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return poolDefault
	}
	return n
}

// guard against accidental import-of-errors removal — the package
// relies on extruntime sentinels which we wrap via fmt.Errorf %w.
var _ = errors.Is
