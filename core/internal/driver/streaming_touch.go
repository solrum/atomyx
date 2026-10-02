package driver

import (
	"context"

	"dev.atomyx/internal/wire"
)

// StreamingTouchCapable is the capability interface for a driver that
// can dispatch individual touch phases (down, move, up) as they
// happen, instead of only a pre-composed gesture. A caller checks
// AsStreamingTouchCapable before routing a mirror drag through this
// path and falls back to the classified touch primitives (Tap,
// LongPress, Swipe, DispatchGesture) when a driver does not implement
// it.
type StreamingTouchCapable interface {
	StreamingTouchDown(ctx context.Context, point wire.Point, id int) error
	StreamingTouchMove(ctx context.Context, point wire.Point, id int) error
	StreamingTouchUp(ctx context.Context, point wire.Point, id int) error
}

// AsStreamingTouchCapable reports whether d also implements
// StreamingTouchCapable, letting a caller route streaming touch
// events through the port package rather than depending on any
// concrete driver implementation. It sees through a decorator.
func AsStreamingTouchCapable(d Driver) (StreamingTouchCapable, bool) {
	return As[StreamingTouchCapable](d)
}
