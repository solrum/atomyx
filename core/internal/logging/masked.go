package logging

import "fmt"

// Masked returns a Logger that passes the message, every string field and
// the error text of each entry through mask before it reaches inner.
// Fields of any other type are logged as they are.
func Masked(inner Logger, mask func(string) string) Logger {
	return maskedLogger{inner: inner, mask: mask}
}

type maskedLogger struct {
	inner Logger
	mask  func(string) string
}

func (l maskedLogger) maskContext(ctx LogContext) LogContext {
	if ctx == nil {
		return nil
	}
	out := make(LogContext, len(ctx))
	for key, value := range ctx {
		if text, ok := value.(string); ok {
			value = l.mask(text)
		}
		out[key] = value
	}
	return out
}

func (l maskedLogger) Debug(msg string, ctx LogContext) {
	l.inner.Debug(l.mask(msg), l.maskContext(ctx))
}

func (l maskedLogger) Info(msg string, ctx LogContext) {
	l.inner.Info(l.mask(msg), l.maskContext(ctx))
}

func (l maskedLogger) Warn(msg string, ctx LogContext) {
	l.inner.Warn(l.mask(msg), l.maskContext(ctx))
}

func (l maskedLogger) Error(msg string, err error, ctx LogContext) {
	if err != nil {
		err = fmt.Errorf("%s", l.mask(err.Error()))
	}
	l.inner.Error(l.mask(msg), err, l.maskContext(ctx))
}

func (l maskedLogger) Child(ctx LogContext) Logger {
	return maskedLogger{inner: l.inner.Child(l.maskContext(ctx)), mask: l.mask}
}
