package androiddriver

import (
	"context"
	"strings"

	"dev.atomyx/internal/process"
)

// adbForward is one entry from "adb forward --list": a serial, the
// host-side endpoint, and the device-side endpoint it forwards to.
type adbForward struct {
	serial string
	local  string
	remote string
}

// listAdbForwards runs "adb forward --list" and parses every forward
// currently registered with adb, across every connected device. A
// host-side local endpoint is a real listening socket on this
// workstation, so at most one forward can hold a given local endpoint
// at a time, regardless of which serial originally created it.
func listAdbForwards(ctx context.Context, proc process.Runner) ([]adbForward, error) {
	result, err := proc.Run(ctx, "adb", "forward", "--list")
	if err != nil {
		return nil, err
	}
	var forwards []adbForward
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		forwards = append(forwards, adbForward{serial: fields[0], local: fields[1], remote: fields[2]})
	}
	return forwards, nil
}

// findAdbForward returns the forward already bound to localPort, if
// any. A local endpoint is a single listening socket, so at most one
// entry can ever match.
func findAdbForward(forwards []adbForward, localPort string) (adbForward, bool) {
	for _, f := range forwards {
		if f.local == localPort {
			return f, true
		}
	}
	return adbForward{}, false
}
