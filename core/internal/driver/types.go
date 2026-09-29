package driver

import "dev.atomyx/internal/wire"

// Capabilities are the static capability flags for a driver
// instance. Callers check these before invoking a conditionally
// supported Driver method and fall back to a portable
// implementation when a flag is false.
type Capabilities struct {
	CanScreenshot     bool `json:"canScreenshot"`
	CanEraseText      bool `json:"canEraseText"`
	CanWaitForIdle    bool `json:"canWaitForIdle"`
	CanSetLocation    bool `json:"canSetLocation"`
	CanSetOrientation bool `json:"canSetOrientation"`
	CanHideKeyboard   bool `json:"canHideKeyboard"`

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
type LaunchArgs struct {
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`

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
