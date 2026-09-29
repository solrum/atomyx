package logging

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level is a log severity, ordered from least to most severe.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String renders level as the lowercase name a log line's "level"
// field carries. An unrecognized value (only reachable by constructing
// a Level outside the declared constants) renders as "info", the same
// fallback levelFromString uses for an unrecognized environment value.
func (level Level) String() string {
	switch level {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// levelEnvVar names the environment variable a host sets to choose the
// minimum severity a StructuredLogger writes.
const levelEnvVar = "ATOMYX_LOG_LEVEL"

// levelFromString parses raw as a Level, matching case-insensitively
// and ignoring surrounding whitespace. It reports false for any value
// that names no recognized level, so a caller can apply its own
// default instead of failing the process over a malformed setting.
func levelFromString(raw string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	case "error":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}

// levelFromEnv reads levelEnvVar, defaulting to LevelInfo when the
// variable is unset or names no recognized level.
func levelFromEnv() Level {
	raw, ok := os.LookupEnv(levelEnvVar)
	if !ok {
		return LevelInfo
	}
	level, ok := levelFromString(raw)
	if !ok {
		return LevelInfo
	}
	return level
}

// StructuredLogger is the Logger every Atomyx binary writes through in
// production: one JSON object per line, filtered by a minimum
// severity, written to an underlying writer.
//
// A StructuredLogger returned by Child shares its parent's writer,
// level, and write mutex, and carries the parent's fields merged with
// its own, so every descendant serializes onto the same writer without
// interleaving two entries' bytes.
type StructuredLogger struct {
	out   io.Writer
	level Level
	ctx   LogContext
	mu    *sync.Mutex
}

var _ Logger = (*StructuredLogger)(nil)

// NewStructuredLogger builds a StructuredLogger writing to out,
// dropping any call below level before it is encoded.
func NewStructuredLogger(out io.Writer, level Level) *StructuredLogger {
	return &StructuredLogger{out: out, level: level, mu: &sync.Mutex{}}
}

// FromEnv builds the StructuredLogger every composition root
// constructs by default: writes to stderr, so a run's log trace never
// interleaves with the newline-delimited JSON wire protocol
// atomyx-runtime speaks on stdout, at the severity levelEnvVar names
// (falling back to LevelInfo when the variable is unset or names no
// recognized level).
func FromEnv() *StructuredLogger {
	return NewStructuredLogger(os.Stderr, levelFromEnv())
}

// Level reports the minimum severity this logger writes.
func (l *StructuredLogger) Level() Level {
	return l.level
}

func (l *StructuredLogger) Debug(msg string, ctx LogContext) {
	l.write(LevelDebug, msg, nil, ctx)
}

func (l *StructuredLogger) Info(msg string, ctx LogContext) {
	l.write(LevelInfo, msg, nil, ctx)
}

func (l *StructuredLogger) Warn(msg string, ctx LogContext) {
	l.write(LevelWarn, msg, nil, ctx)
}

func (l *StructuredLogger) Error(msg string, err error, ctx LogContext) {
	l.write(LevelError, msg, err, ctx)
}

// Child returns a StructuredLogger carrying ctx merged over this
// logger's own fields, so a key ctx sets overrides the same key
// inherited from the parent. The returned logger shares this logger's
// writer and write mutex, so entries from a parent and any of its
// children never interleave mid-line.
func (l *StructuredLogger) Child(ctx LogContext) Logger {
	return &StructuredLogger{
		out:   l.out,
		level: l.level,
		ctx:   mergeContext(l.ctx, ctx),
		mu:    l.mu,
	}
}

// logLine is the on-the-wire shape of one structured log entry. Field
// order and names are part of the log stream's contract: an operator
// or a log-aggregation pipeline parses these lines, so a field rename
// or reorder is a breaking change to that surface.
type logLine struct {
	Time   string     `json:"time"`
	Level  string     `json:"level"`
	Msg    string     `json:"msg"`
	Error  string     `json:"error,omitempty"`
	Fields LogContext `json:"fields,omitempty"`
}

func (l *StructuredLogger) write(level Level, msg string, err error, ctx LogContext) {
	if level < l.level {
		return
	}
	line := logLine{
		Time:   time.Now().UTC().Format(time.RFC3339Nano),
		Level:  level.String(),
		Msg:    msg,
		Fields: mergeContext(l.ctx, ctx),
	}
	if err != nil {
		line.Error = err.Error()
	}
	encoded, marshalErr := json.Marshal(line)
	if marshalErr != nil {
		return
	}
	encoded = append(encoded, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(encoded)
}

// mergeContext combines base and overlay into a new LogContext, a key
// present in both keeping overlay's value. It returns nil, so a
// logLine with no fields at all omits "fields" entirely rather than
// rendering an empty object, when both inputs are empty.
func mergeContext(base, overlay LogContext) LogContext {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := make(LogContext, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}
