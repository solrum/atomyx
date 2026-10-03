package device

import (
	"context"
	"fmt"
	"strings"
)

// defaultAndroidSerial is the serial adb assigns to a single booted
// emulator, used when a caller names no Android device.
const defaultAndroidSerial = "emulator-5554"

// DeviceNotFoundError reports that a device a caller named, or asked to
// be inferred, is not reachable. A caller maps it to its own user-facing
// error surface. ID is empty when the caller named no device, and Reason
// then says why none could be inferred.
type DeviceNotFoundError struct {
	Platform Platform
	ID       string
	Reason   string
}

// Error implements error.
func (e *DeviceNotFoundError) Error() string {
	if e.ID == "" && e.Reason != "" {
		return fmt.Sprintf("no %s device was named and none could be inferred: %s", e.Platform, e.Reason)
	}
	return fmt.Sprintf("no reachable %s device with id %q", e.Platform, e.ID)
}

// ResolveDescriptor builds the Descriptor a caller targets from a
// platform and an optional device id, deciding the device kind from
// the device's identity rather than from a caller-supplied hint.
//
// Platform notes:
//
//   - Android: the id is used as given, defaulting to the serial adb
//     assigns a single booted emulator when empty. The kind is the
//     one probe reports for a listed serial. A serial the probe does
//     not list, such as one reachable over the network, or a nil
//     probe, gets the kind its name implies: an emulator-prefixed
//     serial is an emulator and any other is a physical device. The
//     id is never rejected, because the Android driver only needs a
//     serial adb accepts.
//   - iOS with an id: probe is scanned for a reachable iOS device
//     whose id matches, ignoring case, and the match's Kind
//     (simulator or physical device) is returned. An id no scan finds
//     returns a *DeviceNotFoundError, because a driver built with a
//     guessed kind either dials a control port nothing serves or
//     skips the USB tunnel a physical device needs.
//   - iOS without an id: the device comes from the runner served on
//     the default control port, asked through runners. A runner that
//     reports a simulator udid or a target device udid names that
//     device. A runner that reports neither was served by hand for a
//     physical device, so the single connected iPhone is used. Either
//     Descriptor carries the default control port, so nothing is
//     launched. No answering runner, or a runner that names no device
//     while the number of connected iPhones is not exactly one,
//     returns a *DeviceNotFoundError with an empty id.
func ResolveDescriptor(ctx context.Context, probe Probe, runners RunnerIdentifier, platform Platform, id string) (Descriptor, error) {
	if platform != PlatformIOS {
		if id == "" {
			id = defaultAndroidSerial
		}
		return Descriptor{ID: id, Platform: PlatformAndroid, Kind: resolveAndroidKind(ctx, probe, id)}, nil
	}
	if id == "" {
		return resolveServedRunner(ctx, probe, runners)
	}
	for _, d := range probe.Scan(ctx) {
		if d.Platform == PlatformIOS && strings.EqualFold(d.ID, id) {
			return Descriptor{ID: d.ID, Platform: PlatformIOS, Name: d.Name, Kind: d.Kind, State: d.State}, nil
		}
	}
	return Descriptor{}, &DeviceNotFoundError{Platform: PlatformIOS, ID: id}
}

// resolveAndroidKind reports the kind of the Android device id, from
// probe when it lists the serial and from the serial's own shape
// otherwise.
func resolveAndroidKind(ctx context.Context, probe Probe, id string) Kind {
	if probe != nil {
		for _, d := range probe.Scan(ctx) {
			if d.Platform == PlatformAndroid && d.ID == id {
				return d.Kind
			}
		}
	}
	return androidKind(id)
}

// resolveServedRunner names the device behind the runner answering on
// the default control port.
func resolveServedRunner(ctx context.Context, probe Probe, runners RunnerIdentifier) (Descriptor, error) {
	notFound := func(reason string) error {
		return &DeviceNotFoundError{Platform: PlatformIOS, Reason: reason}
	}
	if runners == nil {
		return Descriptor{}, notFound("no runner is being served")
	}
	got, ok := runners.IdentifyRunner(ctx, iosControlPort)
	if !ok {
		return Descriptor{}, notFound(fmt.Sprintf("no runner answers on port %d", iosControlPort))
	}
	switch {
	case got.SimulatorUDID != "":
		return Descriptor{ID: got.SimulatorUDID, Platform: PlatformIOS, Kind: KindSimulator, State: StateOnline, Port: iosControlPort}, nil
	case got.TargetUDID != "":
		return Descriptor{ID: got.TargetUDID, Platform: PlatformIOS, Kind: KindDevice, State: StateOnline, Port: iosControlPort}, nil
	}
	var phones []Descriptor
	if probe != nil {
		for _, d := range probe.Scan(ctx) {
			if d.Platform == PlatformIOS && d.Kind == KindDevice && (d.State == "" || d.State == StateOnline) {
				phones = append(phones, d)
			}
		}
	}
	if len(phones) != 1 {
		return Descriptor{}, notFound(fmt.Sprintf("the served runner names no device and %d iPhones are connected", len(phones)))
	}
	d := phones[0]
	return Descriptor{ID: d.ID, Platform: PlatformIOS, Name: d.Name, Kind: KindDevice, State: d.State, Port: iosControlPort}, nil
}
