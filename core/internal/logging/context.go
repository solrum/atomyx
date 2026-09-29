package logging

import "context"

// runIDContextKey is an unexported type so a value WithRunID stores
// can never collide with a context key set by another package.
type runIDContextKey struct{}

// WithRunID returns a copy of ctx that carries id as the active run
// id. Any code holding a context derived from the result, at any
// depth, can recover id through RunIDFromContext without id being
// threaded through its own function signature.
func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, id)
}

// RunIDFromContext reports the run id ctx carries, and whether one is
// present. It reports false both when ctx carries no run id at all
// and when WithRunID was called with an empty id, since an empty run
// id correlates nothing.
func RunIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(runIDContextKey{}).(string)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// ChildFromContext returns logger extended with ctx's run id under
// the "runId" field, when ctx carries one via WithRunID, so a call
// site can log a run-correlated entry without threading the run id
// through its own parameters. It returns logger unchanged when ctx
// carries no run id.
func ChildFromContext(logger Logger, ctx context.Context) Logger {
	if runID, ok := RunIDFromContext(ctx); ok {
		return logger.Child(LogContext{"runId": runID})
	}
	return logger
}
