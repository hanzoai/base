package logger

import "log/slog"

// SlogAdapter wraps a *slog.Logger so that it satisfies Logger. It is
// the only adapter that exposes the underlying slog.Handler — that
// escape hatch is required by the in-app log batcher (BatchHandler)
// and by tests that need to type-assert the handler.
type SlogAdapter struct {
	l *slog.Logger
}

// NewSlog wraps the given *slog.Logger. Pass slog.New(handler) here.
func NewSlog(l *slog.Logger) *SlogAdapter {
	if l == nil {
		l = slog.Default()
	}
	return &SlogAdapter{l: l}
}

// Slog returns the wrapped *slog.Logger. Reserved for the BatchHandler
// plumbing — do not call from business logic.
func (s *SlogAdapter) Slog() *slog.Logger { return s.l }

// Handler returns the underlying slog.Handler, mainly so that
// BatchHandler can be reached for SetLevel during settings-reload.
func (s *SlogAdapter) Handler() slog.Handler { return s.l.Handler() }

// Debug logs at debug level.
func (s *SlogAdapter) Debug(msg string, kv ...any) { s.l.Debug(msg, kv...) }

// Info logs at info level.
func (s *SlogAdapter) Info(msg string, kv ...any) { s.l.Info(msg, kv...) }

// Warn logs at warn level.
func (s *SlogAdapter) Warn(msg string, kv ...any) { s.l.Warn(msg, kv...) }

// Error logs at error level.
func (s *SlogAdapter) Error(msg string, kv ...any) { s.l.Error(msg, kv...) }

// With returns a child logger seeded with the given key/value pairs.
func (s *SlogAdapter) With(kv ...any) Logger {
	return &SlogAdapter{l: s.l.With(kv...)}
}
