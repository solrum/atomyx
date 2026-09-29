package mock

import (
	"context"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// StreamingDriver embeds Driver and additionally implements
// driver.StreamingTouchCapable, for a test that wants to exercise the
// optional streaming-touch path without a real device. Driver itself
// does not implement driver.StreamingTouchCapable, matching an
// adapter build that supports only the classified touch primitives
// (Tap, LongPress, Swipe, DispatchGesture); a test picks New or
// NewStreamingTouchCapable depending on which branch it needs.
type StreamingDriver struct {
	*Driver

	// StreamingCalls records every streaming touch phase in dispatch
	// order, keyed by phase name ("StreamingTouchDown",
	// "StreamingTouchMove", "StreamingTouchUp").
	StreamingCalls []Call
}

// NewStreamingTouchCapable constructs a StreamingDriver with the same
// default capability set as New.
func NewStreamingTouchCapable() *StreamingDriver {
	return &StreamingDriver{Driver: New()}
}

var _ driver.Driver = (*StreamingDriver)(nil)
var _ driver.StreamingTouchCapable = (*StreamingDriver)(nil)

func (d *StreamingDriver) StreamingTouchDown(ctx context.Context, point wire.Point, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.StreamingCalls = append(d.StreamingCalls, Call{Method: "StreamingTouchDown", Args: []any{point, id}})
	return nil
}

func (d *StreamingDriver) StreamingTouchMove(ctx context.Context, point wire.Point, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.StreamingCalls = append(d.StreamingCalls, Call{Method: "StreamingTouchMove", Args: []any{point, id}})
	return nil
}

func (d *StreamingDriver) StreamingTouchUp(ctx context.Context, point wire.Point, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.StreamingCalls = append(d.StreamingCalls, Call{Method: "StreamingTouchUp", Args: []any{point, id}})
	return nil
}
