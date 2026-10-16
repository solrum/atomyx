package androiddriver

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// androidRotations maps each neutral orientation to the system rotation
// value Android stores in the user_rotation setting.
//
// Android names a rotation by how far the display content turns, which
// is the opposite of how far the device turns. Rotation 1 is the device
// turned a quarter counterclockwise, so its top edge points left, and
// rotation 3 is the device turned a quarter clockwise. Rotation 2, the
// upside-down portrait, is not offered because phones ignore it.
var androidRotations = map[driver.Orientation]string{
	driver.OrientationPortrait:       "0",
	driver.OrientationLandscapeLeft:  "1",
	driver.OrientationLandscapeRight: "3",
}

// SetOrientation turns automatic rotation off, stores the rotation the
// orientation maps to, then reads the screen size until it reflects the
// new layout.
//
// Automatic rotation stays off and the rotation stays fixed after the
// call, because nothing restores the earlier settings. The screen size
// is derived from the live hierarchy on every read, so no cached size
// needs invalidating.
func (d *AndroidDriver) SetOrientation(ctx context.Context, orientation driver.Orientation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := driver.ValidateOrientation(orientation); err != nil {
		return err
	}
	for _, setting := range [][]string{
		{"accelerometer_rotation", "0"},
		{"user_rotation", androidRotations[orientation]},
	} {
		if err := d.putSystemSetting(ctx, setting[0], setting[1]); err != nil {
			return err
		}
	}
	return driver.AwaitOrientation(ctx, d.opts.Clock, orientation, d.ScreenSize)
}

// putSystemSetting writes one value to the system settings table
// through the host shell. The name and value are fixed words the driver
// chooses, so they need no quoting.
func (d *AndroidDriver) putSystemSetting(ctx context.Context, name, value string) error {
	res, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "shell", "settings", "put", "system", name, value)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
		return err
	}
	reason := firstOutputLine(res.Stderr, res.Stdout)
	if adbDeviceUnreachable(reason) {
		return d.unreachable(err)
	}
	message := "the device refused the rotation setting"
	if reason != "" {
		message += ": " + reason
	}
	return diag.NewOrientationNotSet(message, err)
}
