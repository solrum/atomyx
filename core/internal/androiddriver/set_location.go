package androiddriver

import (
	"context"
	"strconv"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// emulatorSerialPrefix starts the adb serial of every emulator instance.
const emulatorSerialPrefix = "emulator-"

// emulatorConsoleRefusal starts the line the emulator console prints when
// it rejects a command. The console reports the refusal in its output
// and still lets adb exit with status 0.
const emulatorConsoleRefusal = "KO"

// isEmulatorSerial reports whether serial names an emulator instance.
func isEmulatorSerial(serial string) bool {
	return strings.HasPrefix(serial, emulatorSerialPrefix)
}

// SetLocation sends the position to the emulator console. The console
// takes the longitude first, then the latitude, which is the reverse of
// the order the port uses.
//
// A physical device has no console to send the position to. Moving it
// needs a mock location provider installed on the device, so the call
// fails with diag.CodeLocationUnsupported.
func (d *AndroidDriver) SetLocation(ctx context.Context, location driver.Location) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.capabilities.CanSetLocation {
		return diag.NewLocationUnsupported(
			"the location of a physical device cannot be set from the host, because it needs a mock location provider installed on the device")
	}
	if err := driver.ValidateLocation(location); err != nil {
		return err
	}
	res, err := d.proc.Run(ctx, "adb", emulatorGeoFixArguments(d.opts.Serial, location)...)
	if err != nil {
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
		return diag.NewLocationNotSet("the emulator console refused the position"+reasonSuffix(reason), err)
	}
	if refusal, refused := consoleRefusal(res.Stdout, res.Stderr); refused {
		return diag.NewLocationNotSet("the emulator console refused the position"+reasonSuffix(refusal), nil)
	}
	return nil
}

// emulatorGeoFixArguments builds the adb command line for the console
// command "geo fix <longitude> <latitude>". The numbers are formatted
// without an exponent, so they reach the console as plain decimals and
// need no quoting.
func emulatorGeoFixArguments(serial string, location driver.Location) []string {
	return []string{
		"-s", serial, "emu", "geo", "fix",
		strconv.FormatFloat(location.Longitude, 'f', -1, 64),
		strconv.FormatFloat(location.Latitude, 'f', -1, 64),
	}
}

// consoleRefusal reports whether the output holds a refusal line, and
// returns the console's explanation, which may be empty.
func consoleRefusal(outputs ...string) (string, bool) {
	for _, output := range outputs {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line == emulatorConsoleRefusal || strings.HasPrefix(line, emulatorConsoleRefusal+":") {
				return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, emulatorConsoleRefusal), ":")), true
			}
		}
	}
	return "", false
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}
