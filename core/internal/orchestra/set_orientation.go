package orchestra

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// SetOrientation rotates the device to orientation and returns once the
// screen reports the rotated layout. An unknown orientation fails with
// an error coded diag.CodeInvalidArgument before any device call. It
// fails with diag.CodeOrientationUnsupported when the device cannot be
// rotated, and with diag.CodeOrientationNotSet when the screen does not
// take the layout.
//
// The rotation persists after the call, so a caller that changes it puts
// it back.
func (o *Orchestra) SetOrientation(ctx context.Context, orientation driver.Orientation) error {
	if err := driver.ValidateOrientation(orientation); err != nil {
		return err
	}
	if !o.driver.Capabilities().CanSetOrientation {
		return diag.NewOrientationUnsupported("")
	}
	return o.driver.SetOrientation(ctx, orientation)
}
