package logging

import "context"

// runIDContextKey is an unexported type so a value WithRunID stores
// can never collide with a context key set by another package.
type runIDContextKey struct{}

// WithRunID returns a copy of ctx that carries id as the active run
// id. Any code holding a context derived from the result, at any
// depth, can recover id through RunIDFromContext without id being
// threaded through its own function signature.
func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runIDContextKey{}, id)
}

// RunIDFromContext reports the run id ctx carries, and whether one is
// present. It reports false both when ctx carries no run id at all
// and when WithRunID was called with an empty id, since an empty run
// id correlates nothing.
func RunIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(runIDContextKey{}).(string)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// deviceContextKey is an unexported type so a value WithDevice stores
// can never collide with a context key set by another package.
type deviceContextKey struct{}

// DeviceTag names the device a piece of work drives, so a log line
// from any depth can say which of several concurrent devices it
// concerns.
type DeviceTag struct {
	// DeviceID is the id the host reaches the device by, such as an adb
	// serial or an iOS UDID.
	DeviceID string
	// HardwareID is the canonical hardware identity the device lease
	// keys on. It can differ from DeviceID, for example for a phone
	// reached over Wi-Fi.
	HardwareID string
}

// WithDevice returns a copy of ctx that carries tag. Any code holding
// a context derived from the result can recover it through
// DeviceFromContext without the tag being threaded through its own
// signature.
func WithDevice(ctx context.Context, tag DeviceTag) context.Context {
	return context.WithValue(ctx, deviceContextKey{}, tag)
}

// DeviceFromContext reports the device tag ctx carries, and whether one
// is present. It reports false when ctx carries none or when both
// fields are empty, since an empty tag identifies nothing.
func DeviceFromContext(ctx context.Context) (DeviceTag, bool) {
	tag, ok := ctx.Value(deviceContextKey{}).(DeviceTag)
	if !ok || (tag.DeviceID == "" && tag.HardwareID == "") {
		return DeviceTag{}, false
	}
	return tag, true
}

// ChildFromContext returns logger extended with the correlation fields
// ctx carries: the run id under "runId" (see WithRunID), and the
// device under "deviceId" and "hardwareId" (see WithDevice). A call
// site can log a correlated entry without threading either through its
// own parameters. It returns logger unchanged when ctx carries neither.
func ChildFromContext(logger Logger, ctx context.Context) Logger {
	fields := LogContext{}
	if runID, ok := RunIDFromContext(ctx); ok {
		fields["runId"] = runID
	}
	if tag, ok := DeviceFromContext(ctx); ok {
		if tag.DeviceID != "" {
			fields["deviceId"] = tag.DeviceID
		}
		if tag.HardwareID != "" {
			fields["hardwareId"] = tag.HardwareID
		}
	}
	if len(fields) == 0 {
		return logger
	}
	return logger.Child(fields)
}
