package extruntime

import (
	"context"
	"fmt"
	"sync"
)

// NativeFunc is the signature every native extension function must satisfy.
type NativeFunc func(ctx context.Context, payload []byte) ([]byte, error)

// NativeRegistry is a process-wide registry of compiled-in native Go
// extension functions, keyed by "<extension-name>:<fn-name>". The native
// runtime resolves Invoke() against this map. Tests and benchmarks
// register fixtures here; production code links extensions in at build
// time the same way.
var NativeRegistry = struct {
	mu sync.RWMutex
	m  map[string]NativeFunc
}{m: map[string]NativeFunc{}}

// RegisterNative adds a native function to the registry. Safe to call
// from init() in extension packages.
func RegisterNative(ext, fn string, f NativeFunc) {
	NativeRegistry.mu.Lock()
	defer NativeRegistry.mu.Unlock()
	NativeRegistry.m[ext+":"+fn] = f
}

// NewNative returns a native Go runtime. Code is linked into the binary
// at compile time; the runtime just looks functions up.
func NewNative() Runtime { return &nativeRuntime{} }

type nativeRuntime struct{}

func (*nativeRuntime) Name() string { return "native" }
func (*nativeRuntime) Capabilities() Capabilities {
	return Capabilities{
		AcceptsLanguages: []string{"go"},
		HardSandbox:      false,
		Cgo:              false,
		SupportsAbort:    false,
	}
}

func (*nativeRuntime) Load(ctx context.Context, dir string) (Module, error) {
	m, err := LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Runtime != "native" {
		return nil, fmt.Errorf("%w: native runtime cannot load %q runtime", ErrUnsupported, m.Runtime)
	}
	return &nativeModule{name: m.Name, exports: m.Exports}, nil
}
func (*nativeRuntime) Close() error { return nil }

type nativeModule struct {
	name    string
	exports []string
	closed  bool
	mu      sync.Mutex
}

func (m *nativeModule) Name() string      { return m.name }
func (m *nativeModule) Runtime() string   { return "native" }
func (m *nativeModule) Exports() []string { return m.exports }

func (m *nativeModule) Invoke(ctx context.Context, fn string, payload []byte) ([]byte, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	m.mu.Unlock()

	NativeRegistry.mu.RLock()
	f, ok := NativeRegistry.m[m.name+":"+fn]
	NativeRegistry.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s:%s", ErrUnknownFn, m.name, fn)
	}
	return f(ctx, payload)
}

func (m *nativeModule) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}
