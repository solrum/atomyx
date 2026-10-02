package process

import "context"

type envKey struct{}

// WithEnv returns a context that makes a command started through it by
// Runner.Run or BackgroundRunner.Start run with env added to the
// environment it inherits from this program. Each entry has the form
// "NAME=value", and an entry replaces an inherited variable of the same
// name. Carrying the environment on the context leaves both method
// signatures unchanged for every existing caller. A call to WithEnv on
// a context that already carries an environment appends to it.
func WithEnv(ctx context.Context, env ...string) context.Context {
	merged := append(append([]string(nil), EnvFrom(ctx)...), env...)
	return context.WithValue(ctx, envKey{}, merged)
}

// EnvFrom returns the additional environment WithEnv attached to ctx,
// or nil when there is none. The returned slice must not be modified.
func EnvFrom(ctx context.Context) []string {
	env, _ := ctx.Value(envKey{}).([]string)
	return env
}
