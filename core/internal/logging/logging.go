package logging

// LogContext carries structured key-value fields attached to one log
// entry, or merged into every entry a Logger returned by Child writes
// afterward.
type LogContext map[string]any

// Logger is the structured logging port the runner and every command
// handler write through, so the host chooses the destination (stderr,
// a file, an editor panel, a remote log service) without the runner
// or any command depending on that choice.
type Logger interface {
	Debug(msg string, ctx LogContext)
	Info(msg string, ctx LogContext)
	Warn(msg string, ctx LogContext)
	// Error logs msg at error severity. err may be nil when the
	// failure has no underlying Go error to attach.
	Error(msg string, err error, ctx LogContext)
	// Child returns a Logger whose ctx is merged into every entry it
	// logs afterward, without the caller re-passing that context at
	// every call site.
	Child(ctx LogContext) Logger
}

// NoopLogger discards every log entry. It is the Logger a host uses
// when it has not wired a real log sink.
type NoopLogger struct{}

var _ Logger = NoopLogger{}

func (NoopLogger) Debug(string, LogContext)        {}
func (NoopLogger) Info(string, LogContext)         {}
func (NoopLogger) Warn(string, LogContext)         {}
func (NoopLogger) Error(string, error, LogContext) {}
func (NoopLogger) Child(LogContext) Logger         { return NoopLogger{} }
