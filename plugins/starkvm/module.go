package starkvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanzoai/base/plugins/extruntime"
	"go.starlark.net/starlark"
)

// module is one loaded starlark extension. The source is parsed and
// compiled at Load into a *starlark.Program. Each Invoke borrows a
// *starlark.Thread from the per-module pool, executes the compiled
// program once on that thread to populate its global frame (cached
// thereafter), then calls the named global function with a JSON-
// decoded payload converted to a starlark.Value.
type module struct {
	name    string
	exports []string
	program *starlark.Program

	// poolSize is the configured size; pool is the actual buffered
	// channel of threads. We use channel-based pooling so Invoke's
	// checkout respects ctx cancellation cleanly (unlike a mutex-
	// guarded slice).
	pool chan *threadEntry

	mu     sync.Mutex
	closed bool
}

// threadEntry pairs a starlark.Thread with its memoized global frame.
// The first Invoke on a fresh thread runs the program and captures
// the resulting StringDict; subsequent invocations on the same thread
// reuse the dict directly without re-running the program.
type threadEntry struct {
	thread  *starlark.Thread
	globals starlark.StringDict
}

// emptyPredeclared is the predeclared environment given to every
// starlark.Program. We intentionally do NOT inject any host bindings
// here — keep the surface minimal. If a future extension manifest
// declares capabilities, this is where we'd map them to functions.
var emptyPredeclared = starlark.StringDict{}

// newModule loads dir/<m.Module>, parses it, and pre-warms a pool.
func newModule(dir string, m *extruntime.Manifest, poolSize int) (*module, error) {
	src := m.Module
	if src == "" {
		src = "validate.star"
	}
	path := filepath.Join(dir, src)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("starkvm: read %s: %w", path, err)
	}

	// SourceProgram parses + resolves once; the resulting *Program is
	// safe to execute on many threads (Init() returns a fresh globals
	// dict each call). This is the heavy compile step we want to do
	// at Load, not per Invoke.
	_, prog, err := starlark.SourceProgramOptions(fileOptions, path, data, func(name string) bool {
		_, ok := emptyPredeclared[name]
		return ok
	})
	if err != nil {
		return nil, fmt.Errorf("starkvm: compile %s: %w", path, err)
	}

	mod := &module{
		name:    m.Name,
		exports: m.Exports,
		program: prog,
		pool:    make(chan *threadEntry, poolSize),
	}
	// Pre-fill the pool with bare entries — globals lazily initialized
	// on first use of each entry.
	for i := 0; i < poolSize; i++ {
		mod.pool <- &threadEntry{thread: newThread(m.Name, i)}
	}
	return mod, nil
}

func newThread(name string, id int) *starlark.Thread {
	t := &starlark.Thread{
		Name: fmt.Sprintf("starkvm:%s#%d", name, id),
		// Discard print() output — guests that want logging must use
		// a host-provided binding, not stdout. Same posture as goja.
		Print: func(_ *starlark.Thread, _ string) {},
	}
	return t
}

func (m *module) Name() string      { return m.name }
func (m *module) Runtime() string   { return "starlark" }
func (m *module) Exports() []string { return m.exports }

func (m *module) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, extruntime.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Checkout — respect ctx so a cancelled caller doesn't wait on
	// a saturated pool indefinitely.
	var ent *threadEntry
	select {
	case ent = <-m.pool:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Return the entry to the pool unless we're closing.
	defer func() {
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return
		}
		m.pool <- ent
	}()

	// Wire ctx cancellation to thread.Cancel. Starlark checks the
	// cancel flag between opcodes; cancellation arrives within a
	// few microseconds for any real script. Reset the flag after
	// the call so a future Invoke on this thread (with a fresh ctx)
	// isn't poisoned by the previous cancel.
	ent.thread.Uncancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			ent.thread.Cancel("ctx cancelled")
		case <-done:
		}
	}()

	// Lazily run the program on this thread to populate globals.
	// Done at most once per pool entry's lifetime — the program is
	// idempotent (it just defines functions); re-running would
	// recompute closures for no gain.
	if ent.globals == nil {
		globals, err := m.program.Init(ent.thread, emptyPredeclared)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("starkvm: init %s: %w", m.name, err)
		}
		globals.Freeze()
		ent.globals = globals
	}

	callable, ok := ent.globals[fn]
	if !ok {
		return nil, fmt.Errorf("%w: %s:%s", extruntime.ErrUnknownFn, m.name, fn)
	}
	if _, isCallable := callable.(starlark.Callable); !isCallable {
		return nil, fmt.Errorf("%w: %s:%s is not callable", extruntime.ErrUnknownFn, m.name, fn)
	}

	// Decode payload JSON to a Go value, convert to starlark.Value.
	var arg any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &arg); err != nil {
			return nil, fmt.Errorf("starkvm: payload not JSON: %w", err)
		}
	}
	skArg, err := goToStarlark(arg)
	if err != nil {
		return nil, fmt.Errorf("starkvm: convert payload: %w", err)
	}

	res, err := starlark.Call(ent.thread, callable, starlark.Tuple{skArg}, nil)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			return nil, fmt.Errorf("starkvm: invoke %s:%s: %s", m.name, fn, evalErr.Backtrace())
		}
		return nil, fmt.Errorf("starkvm: invoke %s:%s: %w", m.name, fn, err)
	}

	// Marshal the result back. Convert starlark.Value -> Go -> JSON.
	goRes, err := starlarkToGo(res)
	if err != nil {
		return nil, fmt.Errorf("starkvm: convert result: %w", err)
	}
	return json.Marshal(goRes)
}

func (m *module) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	// Drain best-effort. Threads are plain Go structs — no Close
	// needed; dropping references lets the GC reclaim.
drain:
	for {
		select {
		case <-m.pool:
		default:
			break drain
		}
	}
	return nil
}

// goToStarlark converts a JSON-decoded Go value to a starlark.Value.
// Mirrors what json.Unmarshal produces: map[string]any, []any,
// float64, bool, string, nil.
func goToStarlark(v any) (starlark.Value, error) {
	switch x := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(x), nil
	case float64:
		// JSON numbers always come in as float64 from encoding/json.
		// Preserve int-ness when the value is integral and in range
		// so Starlark scripts can use `age + 1` without a type error.
		if x == math.Trunc(x) && !math.IsInf(x, 0) && x >= math.MinInt64 && x <= math.MaxInt64 {
			return starlark.MakeInt64(int64(x)), nil
		}
		return starlark.Float(x), nil
	case string:
		return starlark.String(x), nil
	case []any:
		elems := make([]starlark.Value, 0, len(x))
		for _, e := range x {
			ev, err := goToStarlark(e)
			if err != nil {
				return nil, err
			}
			elems = append(elems, ev)
		}
		return starlark.NewList(elems), nil
	case map[string]any:
		d := starlark.NewDict(len(x))
		for k, val := range x {
			vv, err := goToStarlark(val)
			if err != nil {
				return nil, err
			}
			if err := d.SetKey(starlark.String(k), vv); err != nil {
				return nil, err
			}
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unsupported Go type %T", v)
	}
}

// starlarkToGo is the inverse — convert a starlark.Value tree to a
// Go any tree suitable for json.Marshal. Tuples and frozensets get
// flattened to slices.
func starlarkToGo(v starlark.Value) (any, error) {
	switch x := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.Int:
		// int64 first, fall back to big.Int through string for
		// very large numbers. JSON doesn't have arbitrary-precision
		// integers, so we accept the standard-library convention of
		// passing them as strings.
		if i64, ok := x.Int64(); ok {
			return i64, nil
		}
		return x.String(), nil
	case starlark.Float:
		return float64(x), nil
	case starlark.String:
		return string(x), nil
	case *starlark.List:
		out := make([]any, 0, x.Len())
		it := x.Iterate()
		defer it.Done()
		var e starlark.Value
		for it.Next(&e) {
			ev, err := starlarkToGo(e)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		return out, nil
	case starlark.Tuple:
		out := make([]any, 0, len(x))
		for _, e := range x {
			ev, err := starlarkToGo(e)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, item := range x.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("dict key must be string, got %s", item[0].Type())
			}
			vv, err := starlarkToGo(item[1])
			if err != nil {
				return nil, err
			}
			out[string(k)] = vv
		}
		return out, nil
	case *starlark.Set:
		out := make([]any, 0, x.Len())
		it := x.Iterate()
		defer it.Done()
		var e starlark.Value
		for it.Next(&e) {
			ev, err := starlarkToGo(e)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported starlark type %s", v.Type())
	}
}
