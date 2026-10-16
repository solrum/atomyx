package orchestra

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// SetLocation moves the device to location. An out-of-range position
// fails with diag.CodeInvalidArgument before any device call. It fails
// with diag.CodeLocationUnsupported when the device cannot be moved, and
// with diag.CodeLocationNotSet when the device refuses the position.
//
// The position persists after the call, so a caller that moves the
// device puts it back.
func (o *Orchestra) SetLocation(ctx context.Context, location driver.Location) error {
	if err := driver.ValidateLocation(location); err != nil {
		return err
	}
	if !o.driver.Capabilities().CanSetLocation {
		return diag.NewLocationUnsupported("")
	}
	return o.driver.SetLocation(ctx, location)
}

// Travel moves the device through points in order. It sets the first
// point at once, then waits for the time the great-circle distance to
// each next point takes at speedMetersPerSecond before it sets that
// point. The device is left at the last point. The waits run on the
// Orchestra clock, so a test with a fake clock does not wait.
//
// A travel that fails validation, see driver.ValidateTravel, fails with
// diag.CodeInvalidArgument before any device call. A device that cannot
// be moved fails with diag.CodeLocationUnsupported before the first
// position. A failure part way leaves the device at the last point it
// accepted.
func (o *Orchestra) Travel(ctx context.Context, points []driver.Location, speedMetersPerSecond float64) error {
	if err := driver.ValidateTravel(points, speedMetersPerSecond); err != nil {
		return err
	}
	if !o.driver.Capabilities().CanSetLocation {
		return diag.NewLocationUnsupported("")
	}
	if err := o.driver.SetLocation(ctx, points[0]); err != nil {
		return err
	}
	delays := driver.TravelLegDelaysMs(points, speedMetersPerSecond)
	for i, delay := range delays {
		if err := o.clock.Sleep(ctx, delay); err != nil {
			return err
		}
		if err := o.driver.SetLocation(ctx, points[i+1]); err != nil {
			return err
		}
	}
	return nil
}
