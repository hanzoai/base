package extruntime

import (
	"context"
	"sort"
	"sync"
)

// A function is source the host keeps and runs on demand: a JSON payload in, a
// JSON result out, and one channel back to the host for the duration of the
// call. That is the whole contract, which is why it is stated here rather than
// in any one language backend — a second language is a [Register] rather than a
// second design.
//
// It sits next to [Runtime]/[Module], which answer a different question. A
// Module is an extension package on disk, loaded once and invoked many times. A
// function is a row, handed to the Runner as source at the moment it is called,
// so nothing about it is resident between calls and nothing about it is shared
// with the next one.

// Fn is one host capability: JSON in, JSON out — the same bytes wire [Module.Invoke]
// uses, so a runtime binds every capability the same way and reads none of them.
type Fn func(arg []byte) ([]byte, error)

// Host is what a running function may ask of its host, keyed by the name the
// guest reaches it under.
//
// What a function can do is decided by whoever builds the Host and never by the
// runtime, so the same runtime serves a function that may read one collection
// and a function that may do nothing at all.
type Host map[string]Fn

// Names returns the host's capability names, sorted, so a runtime that has to
// render them into guest source renders the same source twice.
func (h Host) Names() []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Runner runs one function call: src is the function's source, payload is its
// JSON argument, and host is the only channel back for the duration of the call.
//
// ctx bounds the wall clock. A Runner must stop a call whose ctx is done and
// report ctx.Err(), because a function that runs forever is one that the process
// serves nobody else during.
type Runner func(ctx context.Context, src string, payload []byte, host Host) ([]byte, error)

var runners = struct {
	mu sync.RWMutex
	m  map[string]Runner
}{m: map[string]Runner{}}

// Register adds the Runner for a language ("js"). Safe to call from an init, and
// the last registration for a language wins.
func Register(lang string, r Runner) {
	runners.mu.Lock()
	defer runners.mu.Unlock()
	runners.m[lang] = r
}

// Lookup returns the Runner registered for lang, or nil when a deployment links
// no backend for it.
func Lookup(lang string) Runner {
	runners.mu.RLock()
	defer runners.mu.RUnlock()
	return runners.m[lang]
}
