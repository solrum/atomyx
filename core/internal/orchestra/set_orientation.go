package orchestra

import (
	"context"
	"errors"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// OrientationIdleTimeoutMs bounds how long SetOrientation waits for the
// screen to stop moving after it reports the rotated layout.
const OrientationIdleTimeoutMs = 3000

// SetOrientation rotates the device to orientation and returns once the
// screen reports the rotated layout and has stopped moving, so the
// caller can read the hierarchy or tap immediately afterwards. An
// unknown orientation fails with an error coded diag.CodeInvalidArgument
// before any device call. It fails with diag.CodeOrientationUnsupported
// when the device cannot be rotated, and with diag.CodeOrientationNotSet
// when the screen does not take the layout.
//
// The wait for the screen to stop moving is bounded by
// OrientationIdleTimeoutMs. A screen that is still moving when the bound
// passes does not fail the call, because the rotation itself succeeded.
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
	if err := o.driver.SetOrientation(ctx, orientation); err != nil {
		return err
	}
	// Drivers report the new size when the rotation animation starts, so
	// frames read right away can still be moving. The idle wait runs here
	// because it works on every platform through the capability fallback.
	// Only a stopped caller ends the wait with an error: the rotation has
	// already taken effect, and a hierarchy read that fails while the
	// foreground view is recreated is not a reason to fail it.
	if _, err := o.WaitForIdle(ctx, OrientationIdleTimeoutMs); err != nil && ctxDone(ctx, err) {
		return err
	}
	return nil
}

func ctxDone(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
