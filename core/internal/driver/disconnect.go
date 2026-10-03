package driver

import "context"

// ContextDisconnecter is implemented by a driver whose Disconnect does
// work that can outlive the teardown it belongs to, such as a command run
// against the device. DisconnectContext skips or abandons that work once
// ctx is done, so the teardown never leaves it running past the moment
// the device is given up.
type ContextDisconnecter interface {
	DisconnectContext(ctx context.Context) error
}

// DisconnectWithin disconnects d, bounded by ctx when d, or a driver it
// decorates, implements ContextDisconnecter, and through Disconnect
// otherwise.
func DisconnectWithin(ctx context.Context, d Driver) error {
	if c, ok := As[ContextDisconnecter](d); ok {
		return c.DisconnectContext(ctx)
	}
	return d.Disconnect()
}
