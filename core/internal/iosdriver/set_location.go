package iosdriver

import (
	"context"
	"strconv"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// SetLocation moves the reported position of the target. A simulator is
// moved through the host's location service for the simulator, which
// works without the runner. A physical device is moved by the runner,
// which sets the device location through XCUITest.
//
// A physical device on an OS older than iOS 16.4 fails with
// diag.CodeLocationUnsupported, because the runner cannot set the
// position there. The position persists on the target after the call.
func (d *IosDriver) SetLocation(ctx context.Context, location driver.Location) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.capabilities.CanSetLocation {
		return diag.NewLocationUnsupported("this kind of device cannot have its location set")
	}
	if err := driver.ValidateLocation(location); err != nil {
		return err
	}
	if d.opts.Kind == KindSimulator {
		return d.setSimulatorLocation(ctx, location)
	}
	return d.setDeviceLocation(ctx, location)
}

type setLocationResponse struct {
	Set    bool   `json:"set"`
	Reason string `json:"reason"`
}

// setDeviceLocation asks the runner to set the device location. A reply
// that reports the position unset with the unsupported-os reason maps to
// diag.CodeLocationUnsupported, and any other refusal to
// diag.CodeLocationNotSet.
func (d *IosDriver) setDeviceLocation(ctx context.Context, location driver.Location) error {
	params := map[string]any{"latitude": location.Latitude, "longitude": location.Longitude}
	var resp setLocationResponse
	if err := d.conn.CallInto(ctx, "setLocation", params, &resp); err != nil {
		return d.fail(err)
	}
	if resp.Set {
		return nil
	}
	if resp.Reason == unsupportedOSReason {
		return diag.NewLocationUnsupported("this device needs iOS 16.4 or later to have its location set")
	}
	return diag.NewLocationNotSet("the device refused the position", nil)
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
