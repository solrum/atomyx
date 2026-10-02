package device

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dev.atomyx/internal/process"
)

// Identity is a device's canonical hardware identity: the value a
// lease keys on, independent of how the host reaches the device.
type Identity struct {
	Platform   Platform
	HardwareID string
}

// DeviceStateError reports a device whose identity cannot be read
// because it is not in a usable state, for example an Android device
// that is unauthorized or offline. It is distinct from an empty
// serial, which would otherwise fall through to the emulator
// fallback.
type DeviceStateError struct {
	ID    string
	State State
}

func (e *DeviceStateError) Error() string {
	return fmt.Sprintf("device %s is %s, so its identity cannot be read", e.ID, e.State)
}

// IdentityUnresolvedError reports a device that answered but exposed
// no usable hardware identity.
type IdentityUnresolvedError struct {
	ID string
}

func (e *IdentityUnresolvedError) Error() string {
	return fmt.Sprintf("device %s reports no usable hardware identity", e.ID)
}

// genericAndroidSerials are the serial values emulators and some
// vendor images report for every instance, so they cannot tell two
// devices apart. Comparison is case-insensitive.
var genericAndroidSerials = map[string]bool{
	"":                 true,
	"unknown":          true,
	"null":             true,
	"emulator":         true,
	"android":          true,
	"0123456789abcdef": true,
}

func isGenericSerial(serial string) bool {
	s := strings.ToLower(strings.TrimSpace(serial))
	if genericAndroidSerials[s] {
		return true
	}
	return strings.Trim(s, "0") == ""
}

// IdentityResolver resolves a Descriptor to its canonical Identity
// before any agent connect, through queries with no side effect on the
// device.
type IdentityResolver struct {
	runner process.Runner
}

// NewIdentityResolver constructs an IdentityResolver that queries adb
// through runner.
func NewIdentityResolver(runner process.Runner) *IdentityResolver {
	return &IdentityResolver{runner: runner}
}

// Resolve returns the canonical identity of d.
//
// Android resolves ro.serialno through adb, so one phone reached over
// USB and over Wi-Fi yields one identity. When ro.serialno is empty or
// a known generic value, the identity is the AVD name from
// ro.boot.qemu.avd_name joined with the adb serial. An unauthorized,
// offline or otherwise unavailable device returns a *DeviceStateError
// instead of an empty serial. iOS uses the descriptor's UDID.
func (r *IdentityResolver) Resolve(ctx context.Context, d Descriptor) (Identity, error) {
	switch d.Platform {
	case PlatformIOS:
		if d.ID == "" {
			return Identity{}, &IdentityUnresolvedError{ID: d.ID}
		}
		return Identity{Platform: PlatformIOS, HardwareID: d.ID}, nil
	case PlatformAndroid:
		return r.resolveAndroid(ctx, d)
	default:
		return Identity{}, fmt.Errorf("resolve identity of %s: unsupported platform %q", d.ID, d.Platform)
	}
}

func (r *IdentityResolver) resolveAndroid(ctx context.Context, d Descriptor) (Identity, error) {
	if d.State != "" && d.State != StateOnline {
		return Identity{}, &DeviceStateError{ID: d.ID, State: d.State}
	}
	serial, err := r.getprop(ctx, d, "ro.serialno")
	if err != nil {
		return Identity{}, err
	}
	if !isGenericSerial(serial) {
		return Identity{Platform: PlatformAndroid, HardwareID: serial}, nil
	}
	avd, err := r.getprop(ctx, d, "ro.boot.qemu.avd_name")
	if err != nil {
		return Identity{}, err
	}
	if avd == "" {
		return Identity{}, &IdentityUnresolvedError{ID: d.ID}
	}
	return Identity{Platform: PlatformAndroid, HardwareID: avd + "@" + d.ID}, nil
}

func (r *IdentityResolver) getprop(ctx context.Context, d Descriptor, prop string) (string, error) {
	res, err := r.runner.Run(ctx, "adb", "-s", d.ID, "shell", "getprop", prop)
	if err != nil {
		if state, ok := adbFailureState(res.Stdout + "\n" + res.Stderr + "\n" + err.Error()); ok {
			return "", &DeviceStateError{ID: d.ID, State: state}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", fmt.Errorf("read %s from %s: %w", prop, d.ID, err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// adbFailureState maps adb's failure text to the device state it
// reports.
func adbFailureState(text string) (State, bool) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "unauthorized"):
		return StateUnauthorized, true
	case strings.Contains(lower, "offline"):
		return StateOffline, true
	default:
		return "", false
	}
}
