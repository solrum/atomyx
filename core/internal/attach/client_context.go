package attach

import "context"

type clientKey struct{}

// ClientFromContext returns the name the requesting viewer gave in its
// hello, or an empty string when ctx carries none. A DebugHandler reads it
// to say who acted.
func ClientFromContext(ctx context.Context) string {
	name, _ := ctx.Value(clientKey{}).(string)
	return name
}

func withClient(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, clientKey{}, name)
}
