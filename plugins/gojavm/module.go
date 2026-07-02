package gojavm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	zipruntime "github.com/zap-proto/zip/runtime"

	"github.com/hanzoai/base/plugins/extruntime"
)

// module is one loaded extension. Its (possibly esbuild-bundled) source
// is registered as a CommonJS module in zip's shared *runtime.JSRuntime
// at Load time, reachable from JS via require(moduleKey). Each Invoke
// borrows a pooled VM (inside zip), requires the module, calls the
// requested export with the JSON-decoded payload, and JSON-encodes the
// result. There is no per-module goja.Program or VM bookkeeping here any
// more — zip owns the engine; gojavm owns the manifest + JSON wire.
type module struct {
	js      *zipruntime.JSRuntime
	name    string
	key     string // unique require() key for this module's source
	exports []string

	mu     sync.Mutex
	closed bool
}

// moduleKeySeq makes each loaded module's require() key unique even when
// two manifests share a name, so LoadModule never collides in the shared
// runtime's module table.
var (
	moduleKeyMu  sync.Mutex
	moduleKeySeq int
)

func nextModuleKey(name string) string {
	moduleKeyMu.Lock()
	moduleKeySeq++
	n := moduleKeySeq
	moduleKeyMu.Unlock()
	return fmt.Sprintf("base/ext/%s#%d", name, n)
}

func newModule(js *zipruntime.JSRuntime, dir string, m *extruntime.Manifest) (*module, error) {
	// transpile reads the entry and, for TS / JSX / ESM, bundles the
	// whole dependency graph into one CommonJS program via esbuild. Plain
	// global-function .js is wrapped so its functions become exports.
	src, err := transpile(dir, m)
	if err != nil {
		return nil, err
	}

	key := nextModuleKey(m.Name)
	if err := js.LoadModule(key, src); err != nil {
		return nil, fmt.Errorf("gojavm: load module %s: %w", m.Name, err)
	}

	return &module{
		js:      js,
		name:    m.Name,
		key:     key,
		exports: m.Exports,
	}, nil
}

func (m *module) Name() string      { return m.name }
func (m *module) Runtime() string   { return "goja" }
func (m *module) Exports() []string { return m.exports }

// Invoke runs the module's fn export with the JSON payload and returns
// the JSON-encoded result. The whole call runs inside one VM borrowed
// from zip's pool via a single Eval, so require() resolution, the call
// and the stringify all share one runtime — no cross-VM state.
//
// TODO(zip/runtime): zip's JSRuntime.Eval takes no context.Context, so a
// ctx cancel mid-call cannot interrupt the VM here (the previous
// implementation ran a vm.Interrupt watchdog). Cooperative cancel needs
// an Eval/Invoke-with-ctx on zip's JSRuntime. Tracked on hanzoai/zip
// PR #9 (issues disabled there); until it lands we race Eval against
// ctx.Done() below.
func (m *module) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The driver requires the module, resolves fn (direct export or
	// default-export object property, matching the legacy resolve()),
	// invokes it with the decoded payload and stringifies the result.
	// Errors are surfaced as a thrown JS error -> Eval error.
	payloadJSON := "undefined"
	if len(payload) > 0 {
		if !json.Valid(payload) {
			return nil, fmt.Errorf("gojavm: payload not JSON")
		}
		payloadJSON = string(payload)
	}

	// Resolution order serves every authoring style from one path:
	//   1. CommonJS named export      module.exports.fn
	//   2. CommonJS default export     module.exports.default.fn
	//   3. legacy global assignment    globalThis.fn  (set when the
	//      module body ran `globalThis.fn = ...` — require() above runs
	//      the body, so the global is populated by the time we look).
	// This mirrors the pre-zip resolve() that checked the export object
	// then globalThis. One lookup chain, no caller awareness needed.
	driver := fmt.Sprintf(`(function(){
  var __m = require(%s);
  var __fn =
    (__m && typeof __m[%s] === "function") ? __m[%s] :
    (__m && __m["default"] && typeof __m["default"][%s] === "function") ? __m["default"][%s] :
    (typeof globalThis[%s] === "function") ? globalThis[%s] :
    undefined;
  if (typeof __fn !== "function") { throw new Error(%s); }
  var __r = __fn(%s);
  if (__r && typeof __r.then === "function") {
    throw new Error("gojavm: async handler returned a pending promise; handler must resolve synchronously");
  }
  return __r === undefined ? undefined : JSON.stringify(__r);
})()`,
		jsStr(m.key),
		jsStr(fn), jsStr(fn),
		jsStr(fn), jsStr(fn),
		jsStr(fn), jsStr(fn),
		jsStr(fmt.Sprintf("%s: %s:%s", extruntime.ErrUnknownFn.Error(), m.name, fn)),
		payloadJSON,
	)

	// zip's JSRuntime.Eval has no ctx parameter, so we run it on a
	// goroutine and race it against ctx. On cancel we return ctx.Err()
	// immediately; the borrowed VM keeps running the (now-orphaned) call
	// to completion. For well-behaved handlers that is microseconds. A
	// genuinely runaway handler (infinite loop) leaks one VM + goroutine
	// until it yields — see the TODO above; a ctx-aware Eval on zip's
	// JSRuntime (vm.Interrupt watchdog) is the real fix.
	type evalResult struct {
		val any
		err error
	}
	resCh := make(chan evalResult, 1)
	go func() {
		v, e := m.js.Eval(driver)
		resCh <- evalResult{v, e}
	}()

	var res any
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resCh:
		if r.err != nil {
			// Map the unknown-fn driver throw back to the typed sentinel.
			if isUnknownFnErr(r.err, m.name, fn) {
				return nil, fmt.Errorf("%w: %s:%s", extruntime.ErrUnknownFn, m.name, fn)
			}
			return nil, fmt.Errorf("gojavm: invoke %s:%s: %w", m.name, fn, r.err)
		}
		res = r.val
	}

	switch v := res.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(v), nil
	default:
		// JSON.stringify always yields a string; a non-string result
		// means the driver returned undefined-but-mapped. Re-encode
		// defensively so callers always get valid JSON bytes.
		b, mErr := json.Marshal(v)
		if mErr != nil {
			return nil, nil
		}
		return b, nil
	}
}

func (m *module) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func isUnknownFnErr(err error, name, fn string) bool {
	want := fmt.Sprintf("%s: %s:%s", extruntime.ErrUnknownFn.Error(), name, fn)
	return err != nil && containsSub(err.Error(), want)
}

func containsSub(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// jsStr renders a Go string as a JSON-quoted JS string literal, safe to
// splice into the driver source.
func jsStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
