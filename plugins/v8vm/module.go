//go:build v8vm
// +build v8vm

package v8vm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	v8 "rogchap.com/v8go"

	"github.com/hanzoai/base/plugins/extruntime"
)

// module is one loaded JS extension. It holds a precompiled UnboundScript
// (compiled once into the shared isolate) and a pool of contexts so we
// don't pay context-creation cost per invocation. V8 is single-threaded
// per isolate; the pool exists for *state isolation* between back-to-back
// invocations, not for concurrency.
type module struct {
	rt       *runtime
	name     string
	exports  []string
	origin   string
	unbound  *v8.UnboundScript

	mu     sync.Mutex
	pool   []*v8.Context
	closed bool
}

func newModule(rt *runtime, m *extruntime.Manifest, unbound *v8.UnboundScript) *module {
	return &module{
		rt:      rt,
		name:    m.Name,
		exports: append([]string(nil), m.Exports...),
		origin:  m.Module,
		unbound: unbound,
	}
}

func (m *module) Name() string      { return m.name }
func (*module) Runtime() string     { return "v8go" }
func (m *module) Exports() []string { return append([]string(nil), m.exports...) }

// Invoke runs the named exported function with payload (a JSON document).
//
// Calling convention — much simpler than wasm because V8 has real JS
// objects: we JSON.parse the payload inside the guest context, then call
// globalThis[fn](payload). The result is JSON.stringify'd and returned
// as bytes. The guest defines its function by assigning to globalThis:
//
//	globalThis.validate = function(payload) {
//	    return { ok: true, normalized: payload.email.toLowerCase() };
//	};
//
// Context cancellation hard-aborts via Isolate.TerminateExecution(),
// which raises a non-catchable exception in the running script.
func (m *module) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	m.mu.Unlock()

	if fn == "" {
		return nil, fmt.Errorf("%w: function name is empty", extruntime.ErrUnknownFn)
	}
	if len(payload) == 0 {
		payload = []byte("null")
	}

	v8ctx := m.acquire()
	defer m.release(v8ctx)

	// Wire the cancel watchdog before we cross into V8.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			m.rt.iso.TerminateExecution()
		case <-done:
		}
	}()

	m.rt.lock()
	defer m.rt.unlock()

	// Bind script globals (defines globalThis[fn]) — idempotent if the
	// context is reused; the JS source assigns to globalThis so re-running
	// just refreshes the binding.
	if _, err := m.unbound.Run(v8ctx); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("v8vm: %s: load %s: %w", m.name, m.origin, err)
	}

	// Resolve globalThis[fn].
	global := v8ctx.Global()
	fnVal, err := global.Get(fn)
	if err != nil {
		return nil, fmt.Errorf("v8vm: %s: lookup %s: %w", m.name, fn, err)
	}
	if !fnVal.IsFunction() {
		return nil, fmt.Errorf("%w: %s:%s (not a function)", extruntime.ErrUnknownFn, m.name, fn)
	}
	jsFn, err := fnVal.AsFunction()
	if err != nil {
		return nil, fmt.Errorf("v8vm: %s: as-function %s: %w", m.name, fn, err)
	}

	// Parse payload bytes into a JS value.
	payloadVal, err := v8.JSONParse(v8ctx, string(payload))
	if err != nil {
		return nil, fmt.Errorf("v8vm: %s: parse payload: %w", m.name, err)
	}

	// Call the function with the parsed payload.
	result, err := jsFn.Call(global, payloadVal)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("v8vm: %s:%s: %w", m.name, fn, err)
	}

	// JSON.stringify the return value. Undefined → null so callers always
	// receive valid JSON.
	if result == nil || result.IsUndefined() || result.IsNull() {
		return []byte("null"), nil
	}
	out, err := v8.JSONStringify(v8ctx, result)
	if err != nil {
		return nil, fmt.Errorf("v8vm: %s:%s: stringify: %w", m.name, fn, err)
	}

	// Round-trip-validate the output is well-formed JSON so callers can
	// trust it without re-validating downstream.
	var probe json.RawMessage
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		return nil, fmt.Errorf("v8vm: %s:%s: invalid result json: %w", m.name, fn, err)
	}
	return []byte(out), nil
}

func (m *module) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	for _, c := range m.pool {
		c.Close()
	}
	m.pool = nil
	return nil
}

// acquire returns a context from the pool, or creates a new one if the
// pool is empty. Contexts are bound to the shared isolate.
func (m *module) acquire() *v8.Context {
	m.mu.Lock()
	if n := len(m.pool); n > 0 {
		c := m.pool[n-1]
		m.pool = m.pool[:n-1]
		m.mu.Unlock()
		return c
	}
	m.mu.Unlock()

	m.rt.lock()
	defer m.rt.unlock()
	return v8.NewContext(m.rt.iso)
}

// release returns the context to the pool, capped at the configured
// pool size; surplus contexts are disposed.
func (m *module) release(c *v8.Context) {
	if c == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		c.Close()
		return
	}
	if len(m.pool) < m.rt.poolSize {
		m.pool = append(m.pool, c)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	c.Close()
}
