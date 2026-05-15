package logger

import luxlog "github.com/luxfi/log"

// LuxfiAdapter wraps a github.com/luxfi/log.Logger so that it
// satisfies Logger. The luxfi/log API already uses the geth-style
// variadic key/value pairs, so the adapter is a thin pass-through.
type LuxfiAdapter struct {
	l luxlog.Logger
}

// NewLuxfi wraps the given luxfi/log.Logger. Pass nil to fall back to
// the package default logger.
func NewLuxfi(l luxlog.Logger) *LuxfiAdapter {
	if l == nil {
		// luxlog.NewLogger needs a slog.Handler; for a no-arg adapter
		// we lean on the package's discard handler.
		l = luxlog.New()
	}
	return &LuxfiAdapter{l: l}
}

// Underlying returns the wrapped luxfi/log Logger.
func (a *LuxfiAdapter) Underlying() luxlog.Logger { return a.l }

// Debug logs at debug level.
func (a *LuxfiAdapter) Debug(msg string, kv ...any) { a.l.Debug(msg, kv...) }

// Info logs at info level.
func (a *LuxfiAdapter) Info(msg string, kv ...any) { a.l.Info(msg, kv...) }

// Warn logs at warn level.
func (a *LuxfiAdapter) Warn(msg string, kv ...any) { a.l.Warn(msg, kv...) }

// Error logs at error level.
func (a *LuxfiAdapter) Error(msg string, kv ...any) { a.l.Error(msg, kv...) }

// With returns a child logger seeded with the given key/value pairs.
func (a *LuxfiAdapter) With(kv ...any) Logger {
	return &LuxfiAdapter{l: a.l.New(kv...)}
}
