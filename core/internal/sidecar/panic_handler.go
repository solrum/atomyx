package sidecar

// PanicHandler receives a background goroutine's recovered panic
// value and its stack trace, letting the composition root route a
// crash that would otherwise end the goroutine silently to the
// host's log sink. A nil PanicHandler is treated as a no-op,
// mirroring how Transport treats a nil onError.
type PanicHandler func(source string, recovered any, stack []byte)
