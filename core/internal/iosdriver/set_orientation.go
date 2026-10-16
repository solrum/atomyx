package iosdriver

import (
	"context"

	"dev.atomyx/internal/driver"
)

// SetOrientation asks the runner to rotate the device, then reads the
// screen size until it reflects the new layout. The runner remembers the
// orientation so the gestures it synthesizes afterwards address the
// rotated screen.
//
// A foreground app that does not support the orientation keeps its
// layout, so the wait ends with diag.CodeOrientationNotSet. A simulator
// uses the same runner command as a device.
func (d *IosDriver) SetOrientation(ctx context.Context, orientation driver.Orientation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := driver.ValidateOrientation(orientation); err != nil {
		return err
	}
	if err := d.conn.CallInto(ctx, "setOrientation", map[string]any{"orientation": string(orientation)}, nil); err != nil {
		return d.fail(err)
	}
	return driver.AwaitOrientation(ctx, d.opts.Clock, orientation, d.ScreenSize)
}
