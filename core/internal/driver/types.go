package driver

import "dev.atomyx/internal/wire"

// Capabilities are the static capability flags for a driver
// instance. Callers check these before invoking a conditionally
// supported Driver method and fall back to a portable
// implementation when a flag is false.
type Capabilities struct {
	CanScreenshot   bool `json:"canScreenshot"`
	CanEraseText    bool `json:"canEraseText"`
	CanWaitForIdle  bool `json:"canWaitForIdle"`
	CanSetLocation  bool `json:"canSetLocation"`
	CanHideKeyboard bool `json:"canHideKeyboard"`

	// CanSetOrientation reports whether Driver.SetOrientation is
	// supported. When false, SetOrientation fails with
	// diag.CodeOrientationUnsupported, so a caller can tell an
	// unsupported device from a rotation that did not take.
	CanSetOrientation bool `json:"canSetOrientation"`

	// CanOpenLink reports whether Driver.OpenLink is supported. When
	// false, OpenLink fails with diag.CodeLinkUnsupported, so a caller
	// can tell an unsupported device from a link that failed to open.
	CanOpenLink bool `json:"canOpenLink"`

	// CanSetPermissions reports whether Driver.SetPermissions is
	// supported. When false, SetPermissions fails with
	// diag.CodePermissionsUnsupported, so a caller can tell an
	// unsupported device from a request that failed.
	CanSetPermissions bool `json:"canSetPermissions"`

	// CanQueryForeground reports whether the driver can truthfully
	// answer Driver.CurrentForeground by querying the device for the
	// app actually in the foreground, rather than by reporting a
	// value it only remembers from its own prior launch calls. A
	// caller must check this before relying on CurrentForeground's
	// result; see Driver.CurrentForeground.
	CanQueryForeground bool `json:"canQueryForeground"`

	// CanMultiPointer reports whether the driver can dispatch a
	// gesture with more than one independent pointer path at the
	// same time (pinch, rotate, multi-finger custom). When false, a
	// caller must reject a multi-pointer gesture before dispatch.
	// On iOS the value depends on which event synthesizer the
	// driver selected: only the private XCTest backend reports
	// true.
	CanMultiPointer bool `json:"canMultiPointer"`

	// CanPressure reports whether the driver supports per-waypoint
	// pressure for touches (iOS 3D Touch, Android API 26+
	// willContinue strokes). When false, a caller must reject a
	// gesture carrying a pressure-varying waypoint before dispatch.
	CanPressure bool `json:"canPressure"`

	SupportedKeyCodes []KeyCode `json:"supportedKeyCodes"`
}

// GesturePhase is one waypoint's position in a pointer's lifecycle.
type GesturePhase string

const (
	GesturePhaseDown GesturePhase = "down"
	GesturePhaseMove GesturePhase = "move"
	GesturePhaseUp   GesturePhase = "up"

	// GesturePhaseCapture marks a point in a pointer sequence where the
	// pointer is held in place and a screenshot is taken before the
	// sequence continues. It carries no motion of its own: Point holds
	// the pointer's position at that moment, unchanged from the
	// waypoint before it.
	GesturePhaseCapture GesturePhase = "capture"
)

// GestureWaypoint is one waypoint in a pointer sequence.
// AtOffsetSeconds is measured from the first down waypoint across
// all pointers in a single gesture. Pressure is honored only when
// the driver reports Capabilities.CanPressure.
type GestureWaypoint struct {
	Phase           GesturePhase `json:"phase"`
	Point           wire.Point   `json:"point"`
	AtOffsetSeconds float64      `json:"atOffsetSeconds"`
	Pressure        *float64     `json:"pressure,omitempty"`
}

// GesturePointer is one pointer's time-ordered waypoint list. ID is
// caller-chosen and used only for diagnostics. A gesture with
// multiple pointers aligns them on a shared wall clock anchored at
// the first down waypoint.
type GesturePointer struct {
	ID        string            `json:"id"`
	Waypoints []GestureWaypoint `json:"waypoints"`
}

// Gesture is the cross-platform gesture shape dispatched via
// Driver.DispatchGesture. It covers every W3C Actions sequence: tap,
// long-press, drag, press-and-drag, flick, pinch, rotate, and
// multi-finger custom.
type Gesture struct {
	Pointers []GesturePointer `json:"pointers"`
}

// CapturedImage is one screenshot taken at a GesturePhaseCapture
// waypoint. MimeType names the encoding of Data (for example
// "image/png"), so a caller that routes it into a report or a
// different sink knows how to interpret the bytes without sniffing
// them.
type CapturedImage struct {
	Data     []byte `json:"data"`
	MimeType string `json:"mimeType"`
}

// GestureResult is the outcome of Driver.DispatchGesture. CapturedImages
// holds one entry per GesturePhaseCapture waypoint in the dispatched
// gesture, ordered the same way the gesture's own pointers and their
// waypoints are ordered, so a caller can zip each image back to the
// capture marker that produced it. It is empty when the gesture
// carried no capture waypoint.
type GestureResult struct {
	CapturedImages []CapturedImage `json:"capturedImages,omitempty"`
}

// KeyCode is the cross-platform key vocabulary. Callers use these
// names; a driver translates to its platform-native key codes
// internally. A value outside the named constants is allowed for
// driver-specific keys (Android KEYCODE_ constants, iOS hardware
// keys); a caller that uses one sacrifices portability knowingly.
type KeyCode string

const (
	KeyBack   KeyCode = "back"
	KeyHome   KeyCode = "home"
	KeyEnter  KeyCode = "enter"
	KeyTab    KeyCode = "tab"
	KeyEscape KeyCode = "escape"
	KeyDelete KeyCode = "delete"
	KeySpace  KeyCode = "space"
	KeyUp     KeyCode = "up"
	KeyDown   KeyCode = "down"
	KeyLeft   KeyCode = "left"
	KeyRight  KeyCode = "right"
)

// KeyResult reports whether Driver.PressKey or Driver.HideKeyboard
// used a verifiable affordance. Reason is a freeform, human-readable
// explanation populated on notable outcomes, for example "used:
// nav_bar_back" on success or "no system back on iOS" on failure.
type KeyResult struct {
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// LaunchArgs configures Driver.LaunchApp.
//
// Arguments is the portable way to pass named values to the app at
// launch, and every adapter honors it. Args and Environment are
// process-level inputs that only some platforms can deliver. An
// adapter that cannot deliver a non-empty Args or Environment rejects
// the launch with diag.CodeInvalidArgument instead of dropping it.
type LaunchArgs struct {
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`

	// Arguments maps names to scalar values rendered as strings. They
	// reach the app only when the launch starts a process, so a launch
	// that reuses a running instance ignores them. See
	// ValidateLaunchArguments for the key and value rules.
	Arguments map[string]string `json:"arguments,omitempty"`

	// NoReset selects the launch strategy as a tri-state: see
	// Driver.LaunchApp for the meaning of nil, false, and true.
	NoReset *bool `json:"noReset,omitempty"`
}

// ForegroundInfo reports the app currently in the foreground.
// BundleID is nil when no app is in the foreground.
type ForegroundInfo struct {
	BundleID *string `json:"bundleId"`

	// Activity is the Android activity name when available. It is
	// empty on iOS, which has no equivalent concept.
	Activity string `json:"activity,omitempty"`
}

// InstalledApp identifies one app installed on the device.
type InstalledApp struct {
	BundleID    string `json:"bundleId"`
	DisplayName string `json:"displayName"`
}

// ClearAppDataOptions configures Driver.ClearAppData. Source is the
// artifact path an adapter needs when it has no direct clear
// primitive and must reinstall to reach a clean state; see the
// platform notes on Driver.ClearAppData.
type ClearAppDataOptions struct {
	Source string `json:"source,omitempty"`
}

// OpenLinkOptions configures Driver.OpenLink.
type OpenLinkOptions struct {
	// AppID names the app that must open the link, as an Android
	// package name or an iOS bundle id. Empty lets the device choose
	// the handler.
	AppID string `json:"appId,omitempty"`
}

// DeviceKind classifies the device a driver is attached to.
type DeviceKind string

const (
	DeviceKindSimulator DeviceKind = "simulator"
	DeviceKindEmulator  DeviceKind = "emulator"
	DeviceKindDevice    DeviceKind = "device"
)

// DeviceInfo is static information about the device a driver is
// attached to.
type DeviceInfo struct {
	Platform        string     `json:"platform"`
	PlatformVersion string     `json:"platformVersion"`
	Model           string     `json:"model"`
	UDID            string     `json:"udid"`
	Kind            DeviceKind `json:"kind"`
}
