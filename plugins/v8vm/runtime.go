//go:build v8vm
// +build v8vm

package v8vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	v8 "rogchap.com/v8go"

	"github.com/hanzoai/base/plugins/extruntime"
)

// Defaults — tuned for short-lived per-request JS hooks.
const (
	defaultHeapMBMax   = 256
	defaultHeapMBInit  = 64
	defaultPoolSize    = 8
	envHeapMBMax       = "BASE_V8VM_HEAP_MB_MAX"
	envPoolSize        = "BASE_V8VM_POOL_SIZE"
)

// NewRuntime returns a v8go-backed Runtime. One Isolate is shared across
// every module loaded by this runtime; V8 is not thread-safe per isolate
// so Module.Invoke serializes via a mutex. The shared-isolate trade-off
// is intentional: starting an isolate costs ~10-50ms and 5-20MB of RSS,
// far too much to pay per extension when we typically load 1-50 of them.
func NewRuntime() extruntime.Runtime {
	r := &runtime{
		heapMaxMB: envMB(envHeapMBMax, defaultHeapMBMax),
		poolSize:  envInt(envPoolSize, defaultPoolSize),
	}
	r.iso = v8.NewIsolate()
	return r
}

type runtime struct {
	iso       *v8.Isolate
	heapMaxMB int // advisory soft cap enforced by GetHeapStatistics() watchdog
	poolSize  int

	mu     sync.Mutex // serializes isolate access across all modules
	closed bool
}

func (*runtime) Name() string { return "v8go" }

func (*runtime) Capabilities() extruntime.Capabilities {
	return extruntime.Capabilities{
		AcceptsLanguages: []string{"js"},
		HardSandbox:      true,
		Cgo:              true,
		SupportsAbort:    true,
	}
}

func (r *runtime) Load(_ context.Context, dir string) (extruntime.Module, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, extruntime.ErrClosed
	}
	r.mu.Unlock()

	m, err := extruntime.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Runtime != "v8go" {
		return nil, fmt.Errorf("%w: v8go runtime cannot load %q runtime", extruntime.ErrUnsupported, m.Runtime)
	}
	if m.Module == "" {
		return nil, fmt.Errorf("%w: module path is required", extruntime.ErrBadManifest)
	}

	src, err := os.ReadFile(filepath.Join(dir, m.Module))
	if err != nil {
		return nil, fmt.Errorf("read module %s: %w", m.Module, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	unbound, err := r.iso.CompileUnboundScript(string(src), m.Module, v8.CompileOptions{})
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", m.Module, err)
	}

	return newModule(r, m, unbound), nil
}

func (r *runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.iso != nil {
		r.iso.Dispose()
		r.iso = nil
	}
	return nil
}

// lock guards isolate access for callers — exported within the package
// so module.go can use the same mutex.
func (r *runtime) lock()   { r.mu.Lock() }
func (r *runtime) unlock() { r.mu.Unlock() }

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envMB(key string, def int) int { return envInt(key, def) }
