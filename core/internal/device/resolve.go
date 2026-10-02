package device

import (
	"context"
	"fmt"
	"strings"
)

// defaultAndroidSerial is the serial adb assigns to a single booted
// emulator, used when a caller names no Android device.
const defaultAndroidSerial = "emulator-5554"

// DeviceNotFoundError reports that a device id named by a caller is
// not among the devices a probe found reachable. A caller maps it to
// its own user-facing error surface.
type DeviceNotFoundError struct {
	Platform Platform
	ID       string
}

// Error implements error.
func (e *DeviceNotFoundError) Error() string {
	return fmt.Sprintf("no reachable %s device with id %q", e.Platform, e.ID)
}

// ResolveDescriptor builds the Descriptor a caller targets from a
// platform and an optional device id, deciding the device kind from
// the device's identity rather than from a caller-supplied hint.
//
// Platform notes:
//
//   - Android: the id is used as given, defaulting to the serial adb
//     assigns a single booted emulator when empty. No scan runs,
//     because the Android driver does not depend on the kind and an
//     adb serial reachable over the network is not always listed by
//     a probe.
//   - iOS with an id: probe is scanned for a reachable iOS device
//     whose id matches, ignoring case, and the match's Kind
//     (simulator or physical device) is returned. An id no scan finds
//     returns a *DeviceNotFoundError, because a driver built with a
//     guessed kind either dials a control port nothing serves or
//     skips the USB tunnel a physical device needs.
//   - iOS without an id: a simulator Descriptor with an empty id,
//     which the driver resolves to the booted simulator. No scan
//     runs.
func ResolveDescriptor(ctx context.Context, probe Probe, platform Platform, id string) (Descriptor, error) {
	if platform != PlatformIOS {
		if id == "" {
			id = defaultAndroidSerial
		}
		return Descriptor{ID: id, Platform: PlatformAndroid, Kind: KindEmulator}, nil
	}
	if id == "" {
		return Descriptor{Platform: PlatformIOS, Kind: KindSimulator}, nil
	}
	for _, d := range probe.Scan(ctx) {
		if d.Platform == PlatformIOS && strings.EqualFold(d.ID, id) {
			return Descriptor{ID: d.ID, Platform: PlatformIOS, Name: d.Name, Kind: d.Kind, State: d.State}, nil
		}
	}
	return Descriptor{}, &DeviceNotFoundError{Platform: PlatformIOS, ID: id}
}
