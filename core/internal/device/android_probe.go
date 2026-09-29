package device

import (
	"context"
	"strings"

	"dev.atomyx/internal/process"
)

// AndroidAdbProbe scans devices visible to `adb devices -l`. A
// missing or failing adb is treated as no devices found rather than
// an error, so an Android-less host still gets a usable (empty)
// scan.
type AndroidAdbProbe struct {
	runner process.Runner
}

// NewAndroidAdbProbe constructs an AndroidAdbProbe that shells out
// through runner.
func NewAndroidAdbProbe(runner process.Runner) *AndroidAdbProbe {
	return &AndroidAdbProbe{runner: runner}
}

var _ Probe = (*AndroidAdbProbe)(nil)

// Scan implements Probe.
func (p *AndroidAdbProbe) Scan(ctx context.Context) []Descriptor {
	entries, err := p.listDevices(ctx)
	if err != nil {
		return nil
	}
	out := make([]Descriptor, 0, len(entries))
	for _, e := range entries {
		out = append(out, Descriptor{
			ID:       e.serial,
			Platform: PlatformAndroid,
			Name:     e.serial,
			Kind:     androidKind(e.serial),
			State:    androidState(e.rawState),
		})
	}
	return out
}

func androidKind(serial string) Kind {
	if strings.HasPrefix(serial, "emulator-") {
		return KindEmulator
	}
	return KindDevice
}

// androidState maps one adb device-state token to a device.State. A
// device sitting in the bootloader, recovery, or sideload state is
// reachable by adb but not by USB-debugging commands, and none of
// those three descriptions is an authorization problem, so they map
// to StateUnavailable rather than StateUnauthorized. Any other
// unrecognized token (a real authorization state such as
// "unauthorized" or "no permissions", or a future adb state this
// probe does not yet know about) falls back to StateUnauthorized,
// which is the state that actually needs a user action ("check the
// device screen for a debugging prompt") most of the fallback cases
// share.
func androidState(rawState string) State {
	switch rawState {
	case "device":
		return StateOnline
	case "offline":
		return StateOffline
	case "bootloader", "recovery", "sideload":
		return StateUnavailable
	default:
		return StateUnauthorized
	}
}

// adbDeviceEntry is one line of `adb devices -l` output.
type adbDeviceEntry struct {
	serial   string
	rawState string
}

func (p *AndroidAdbProbe) listDevices(ctx context.Context) ([]adbDeviceEntry, error) {
	result, err := p.runner.Run(ctx, "adb", "devices", "-l")
	if err != nil {
		return nil, err
	}
	return parseAdbDevicesOutput(result.Stdout), nil
}

// parseAdbDevicesOutput parses the output of `adb devices -l`:
//
//	List of devices attached
//	emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64
//	R58N90ABCDE            unauthorized
//
// The first line is a fixed header and is always dropped, regardless
// of its content. A line with fewer than two whitespace-separated
// fields (serial and state) is skipped rather than treated as a
// parse failure, so trailing blank lines don't produce a bogus
// entry.
func parseAdbDevicesOutput(stdout string) []adbDeviceEntry {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) <= 1 {
		return nil
	}
	lines = lines[1:]

	out := make([]adbDeviceEntry, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, adbDeviceEntry{serial: fields[0], rawState: fields[1]})
	}
	return out
}
