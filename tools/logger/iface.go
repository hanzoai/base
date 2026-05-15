package logger

// Logger is the small structured-logging surface used by Base and its
// plugins. It deliberately mirrors the geth/luxfi-style variadic API
// (msg + alternating "key", value pairs) so that the same call sites
// work with both backends.
//
// Two implementations live in this package:
//
//   - slogAdapter (NewSlog) wraps the standard library's *slog.Logger so
//     that the BatchHandler-based on-disk log storage keeps working.
//   - luxfiAdapter (NewLuxfi) wraps github.com/luxfi/log.Logger so that
//     non-storage consumers run through the canonical Lux logger.
//
// Callers should never type-assert to a concrete implementation. For
// the one historical escape hatch (attaching a custom slog.Handler in
// dev mode and in tests) use SlogHandler on the slog-backed adapter.
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)

	// With returns a child logger that prepends the given key/value
	// pairs to every record.
	With(kv ...any) Logger
}
