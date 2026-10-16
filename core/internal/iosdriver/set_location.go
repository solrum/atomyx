package iosdriver

import (
	"context"
	"strconv"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// SetLocation moves the reported position of a simulator through the
// host's location service for the simulator. The adapter reports
// CanSetLocation only for a simulator, and every other kind fails with
// diag.CodeLocationUnsupported before the position is looked at.
//
// The position persists on the simulator after the call.
func (d *IosDriver) SetLocation(ctx context.Context, location driver.Location) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.capabilities.CanSetLocation {
		return diag.NewLocationUnsupported(
			"the location of a physical device cannot be set from the host")
	}
	if err := driver.ValidateLocation(location); err != nil {
		return err
	}
	return d.setSimulatorLocation(ctx, location)
}

// setSimulatorLocation runs the simulator's location command with the
// position as one latitude and longitude pair. The numbers are formatted
// without an exponent so they reach the tool as plain decimals.
func (d *IosDriver) setSimulatorLocation(ctx context.Context, location driver.Location) error {
	pair := strconv.FormatFloat(location.Latitude, 'f', -1, 64) + "," +
		strconv.FormatFloat(location.Longitude, 'f', -1, 64)
	res, err := d.proc.Run(ctx, "xcrun", "simctl", "location", d.opts.UDID, "set", pair)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
		return err
	}
	return diag.NewLocationNotSet("the simulator refused the position: "+
		firstNonEmptyLine(res.Stderr, res.Stdout), err)
}
