package driver

import (
	"context"

	"dev.atomyx/internal/wire"
)

// Driver is the platform-neutral device control surface. Each
// concrete adapter (Android, iOS, a future platform) implements
// this interface as a thin wrapper over its native control channel.
//
// Rule of thumb: if a method can be implemented using other Driver
// methods, it does not belong on this interface. Selector
// resolution, scroll-into-view, obscurement detection, and retry
// stay above this layer.
//
// A caller must check Capabilities before invoking a conditionally
// supported method and fall back to a portable implementation when
// the driver reports the capability absent.
type Driver interface {
	// Platform identifies the concrete adapter: "ios", "android", or
	// a future platform.
	Platform() string

	// Capabilities reports the static capability flags for this
	// driver instance.
	Capabilities() Capabilities

	// Connect opens the transport, performs the handshake, and
	// reaches ready state.
	Connect(ctx context.Context) error

	// Disconnect closes the transport and releases its resources.
	// It is idempotent: calling it while already disconnected does
	// nothing.
	Disconnect() error

	// IsConnected reports whether the transport is connected and
	// ready to serve calls.
	IsConnected() bool

	// Ping performs a bounded liveness probe against the bound agent.
	// The precondition is a device already bound to this driver
	// instance through a prior successful Connect.
	//
	// Ping returns nil when the agent answers before ctx's deadline
	// elapses, and a non-nil error when it does not, meaning the
	// agent is unreachable, the underlying connection was lost, or
	// the deadline elapsed first. Ping has no side effect on the app
	// under test: it never launches, navigates, or captures a
	// snapshot, so a caller may invoke it at any time without
	// disturbing device state.
	Ping(ctx context.Context) error

	// AgentInfo reports the versions the bound agent declared when
	// this driver connected. It is filled by a successful Connect and
	// is the zero AgentInfo before Connect, after a Connect that
	// failed, and whenever the agent does not report a version. It
	// never makes a device call, so a caller may invoke it at any
	// time, and it returns the same value on every call until the
	// next Connect.
	AgentInfo() AgentInfo

	// Hierarchy captures the current UI hierarchy. An implementation
	// must return a fully normalized tree using the canonical
	// attribute key set; no platform-native field name may leak to
	// the caller.
	Hierarchy(ctx context.Context) (wire.TreeNodeWire, error)

	// WaitForIdle blocks until the UI reaches an idle state or
	// timeoutMs elapses, returning true when idle was reached and
	// false on timeout. A caller must only invoke this when
	// Capabilities().CanWaitForIdle is true; otherwise it falls back
	// to tree-diff polling instead.
	WaitForIdle(ctx context.Context, timeoutMs int) (bool, error)

	// Tap dispatches a tap at point.
	Tap(ctx context.Context, point wire.Point) error

	// LongPress dispatches a press-and-hold at point for
	// durationMs.
	LongPress(ctx context.Context, point wire.Point, durationMs int) error

	// Swipe dispatches a linear drag from "from" to "to" over
	// durationMs.
	Swipe(ctx context.Context, from wire.Point, to wire.Point, durationMs int) error

	// DispatchGesture dispatches an arbitrary multi-pointer gesture
	// with time-ordered waypoints, for gestures the primitives above
	// cannot express (press-and-drag, multi-waypoint paths, pinch,
	// rotate, multi-finger custom).
	//
	// An implementation must first reject a malformed gesture by
	// returning the error from ValidateGesture, which carries
	// diag.CodeInvalidArgument. A well-formed gesture has at least
	// one pointer, and every pointer has at least two waypoints that
	// start with down, end with up, contain no other down, never
	// decrease in time offset, use non-negative coordinates, and keep
	// any pressure within [0, 1].
	//
	// An implementation must reject a gesture with more than one
	// pointer when Capabilities().CanMultiPointer is false, and must
	// reject a gesture carrying a waypoint pressure value when
	// Capabilities().CanPressure is false. A rejection is returned
	// as an error whose message is safe to surface to an agent.
	//
	// The returned GestureResult carries one captured image per
	// GesturePhaseCapture waypoint the gesture contains, in dispatch
	// order. An implementation with no way to capture a screenshot
	// while a gesture is in flight must reject a gesture carrying a
	// capture waypoint instead of silently dropping it, returning an
	// error whose code diag.Describe reports as
	// diag.CodePointerCaptureUnsupported.
	DispatchGesture(ctx context.Context, gesture Gesture) (GestureResult, error)

	// InputText types text into whatever element currently has
	// focus.
	InputText(ctx context.Context, text string) error

	// EraseText erases count characters backwards from the focused
	// field. An implementation without native erase support reports
	// Capabilities().CanEraseText false; the caller then falls back
	// to issuing the platform's backspace key count times.
	EraseText(ctx context.Context, count int) error

	// PressKey presses a system or navigation key. The result
	// indicates whether a verifiable affordance was used: iOS has no
	// system back primitive, so there PressKey(KeyBack) may return a
	// result with Ok false and a Reason hinting at falling back to
	// an on-screen affordance.
	PressKey(ctx context.Context, key KeyCode) (KeyResult, error)

	// HideKeyboard dismisses the on-screen keyboard if one is
	// visible, and is a no-op when none is. It returns as soon as
	// the dismissal action is dispatched, not after any dismissal
	// animation completes; a caller that needs to know the keyboard
	// is gone must poll Hierarchy separately. A caller must only
	// invoke this when Capabilities().CanHideKeyboard is true.
	HideKeyboard(ctx context.Context) (KeyResult, error)

	// LaunchApp launches bundleID. args.NoReset selects the launch
	// strategy as a tri-state:
	//
	//   - nil (plain launch): dispatches the launch unconditionally,
	//     with no force-stop and no foreground check. This is the
	//     default for a caller that does not specify a mode.
	//   - false (fresh): stops bundleID first when it is running,
	//     then launches a clean process. This does not clear the
	//     app's on-disk data.
	//   - true (attach): reuses the running instance when bundleID
	//     is already the foreground app, useful for driving an app
	//     without losing in-memory state. Falls back to a plain
	//     launch when bundleID is not currently foreground.
	//
	// args.Arguments is delivered to the app as named values that it
	// can read at startup. A launch that attaches to a running
	// instance ignores them. An invalid arguments map, or a non-empty
	// args.Args or args.Environment on an adapter that cannot deliver
	// them, fails with diag.CodeInvalidArgument before the device is
	// touched.
	//
	// Platform notes (expand this list when adding support):
	//
	//   - Android: Arguments become string extras on the launch
	//     intent. Args and Environment are rejected.
	//   - iOS: Arguments become launch environment entries and
	//     "-name value" launch arguments, which the app reads from
	//     its environment or from its standard user defaults. Args
	//     are appended to the launch arguments and Environment is
	//     merged into the launch environment, with Environment
	//     winning over an Arguments entry of the same name.
	LaunchApp(ctx context.Context, bundleID string, args LaunchArgs) error

	// OpenLink opens link, a URL, deep link or universal link, in the
	// device's handler for it, or in opts.AppID when that is set. It
	// returns once the device accepted the request, not once the
	// handler finished showing a screen, so a caller that needs the
	// destination waits for it with a wait primitive.
	//
	// A caller checks Capabilities().CanOpenLink first. An adapter
	// that reports it false, or whose device software cannot open
	// links, fails with diag.CodeLinkUnsupported. An invalid link,
	// see ValidateLink, fails with diag.CodeInvalidArgument before
	// the device is touched. A link that no app opened fails with
	// diag.CodeLinkNotOpened.
	//
	// Platform notes (expand this list when adding support):
	//
	//   - Android: the host asks the device to start a view intent for
	//     the link, restricted to opts.AppID when set.
	//   - iOS: the runner opens the link through the system, or
	//     through opts.AppID when set. The device needs iOS 16.4 or
	//     later, and an older one fails with diag.CodeLinkUnsupported.
	OpenLink(ctx context.Context, link string, opts OpenLinkOptions) error

	// SetPermissions grants or revokes permissions of appID without a
	// prompt. Each key names a platform-neutral permission group and
	// each value is PermissionAllow or PermissionDeny. The change is
	// in place before the call returns, but an app that is running
	// may keep its earlier view of a permission until it starts
	// again, so a caller applies permissions before launching the app.
	//
	// A caller checks Capabilities().CanSetPermissions first. An
	// adapter that reports it false, or a device on which a requested
	// permission does not exist, fails with
	// diag.CodePermissionsUnsupported. An empty map, an unknown name
	// or an unknown state fails with diag.CodeInvalidArgument before
	// the device is touched, see ValidatePermissions. A request that
	// reached the device but changed nothing, such as an app that is
	// not installed or does not declare the permission, fails with
	// diag.CodePermissionNotSet.
	//
	// Platform notes (expand this list when adding support):
	//
	//   - Android: the host grants or revokes each runtime
	//     permission the name maps to. A group member the app does
	//     not declare is skipped, and the call fails only when the
	//     app declares none of them. Notifications need Android 13
	//     or later.
	//   - iOS simulator: the host sets the privacy service the name
	//     maps to for the app.
	//   - iOS device: not supported, because the system offers no way
	//     to change an app's permissions from outside the app.
	SetPermissions(ctx context.Context, appID string, permissions map[Permission]PermissionState) error

	// SetOrientation rotates the device to orientation and returns once
	// ScreenSize and Hierarchy report the rotated layout: wider than
	// tall for a landscape orientation and taller than wide for
	// portrait. A caller reads the resulting orientation through
	// ScreenSize, so the port has no separate getter. The size cannot
	// tell landscapeLeft from landscapeRight, so a request to turn from
	// one landscape to the other returns as soon as the call is
	// accepted.
	//
	// The rotation persists after the call. Nothing restores the
	// earlier orientation when the session ends, so a caller that
	// changes it puts it back.
	//
	// A caller checks Capabilities().CanSetOrientation first. An adapter
	// that reports it false fails with diag.CodeOrientationUnsupported.
	// An unknown orientation fails with diag.CodeInvalidArgument before
	// the device is touched, see ValidateOrientation. A device that
	// accepted the request but whose screen did not reach the requested
	// layout within OrientationSettleTimeoutMs, for example because the
	// foreground app only supports another orientation, fails with
	// diag.CodeOrientationNotSet.
	//
	// Platform notes (expand this list when adding support):
	//
	//   - Android: the host turns off automatic rotation, then sets the
	//     rotation the orientation maps to. The rotation stays fixed
	//     until the user or another call changes it, and automatic
	//     rotation stays off.
	//   - iOS: the runner sets the device orientation and remembers it
	//     for gestures. An orientation change made by hand is not
	//     observed by the runner until the next SetOrientation.
	SetOrientation(ctx context.Context, orientation Orientation) error

	// SetLocation moves the device's reported position to location. The
	// position persists after the call, and nothing restores the earlier
	// one when the session ends, so a caller that changes it puts it
	// back. An app that was already running receives the new position
	// on its next location update.
	//
	// A caller checks Capabilities().CanSetLocation first. An adapter
	// that reports it false fails with diag.CodeLocationUnsupported
	// before it looks at location. A latitude or longitude outside its
	// range, or one that is not finite, fails with
	// diag.CodeInvalidArgument before the device is touched, see
	// ValidateLocation. A device that was reached but refused the
	// position fails with diag.CodeLocationNotSet.
	//
	// Platform notes (expand this list when adding support):
	//
	//   - Android emulator: the host sends the position to the emulator
	//     console.
	//   - Android device: not supported, because a physical device
	//     needs a mock location provider installed on the device.
	//   - iOS simulator: the host sets the simulator's location.
	//   - iOS device: not supported.
	SetLocation(ctx context.Context, location Location) error

	// StopApp stops bundleID. See CurrentForeground for what the
	// foreground report guarantees once StopApp returns nil.
	StopApp(ctx context.Context, bundleID string) error

	// KillApp forcibly terminates bundleID.
	KillApp(ctx context.Context, bundleID string) error

	// CurrentForeground reports the app currently in the foreground
	// on an adapter whose Capabilities().CanQueryForeground is true.
	// On an adapter whose CanQueryForeground is false, it returns a
	// typed unsupported error instead of a value, because the
	// adapter has no way to query the device for the true foreground
	// app. A caller must check CanQueryForeground before relying on
	// the returned ForegroundInfo.
	//
	// The report reflects the device with a short delay. When
	// LaunchApp returns nil, CurrentForeground reports bundleID in
	// front, and when StopApp returns nil it no longer does, except
	// that the adapter bounds its wait for this, so an app that
	// handed off to another package can leave the report different.
	// A caller that reads the foreground right after any other
	// device action must poll with its own bound, not trust one read.
	CurrentForeground(ctx context.Context) (ForegroundInfo, error)

	// ListApps lists the apps installed on the device.
	ListApps(ctx context.Context) ([]InstalledApp, error)

	// InstallApp installs the app artifact at source onto the
	// current device. source must be a path reachable from the host
	// running this driver (for example an APK on Android, or a .app
	// or .ipa on iOS), never a device-side path.
	InstallApp(ctx context.Context, source string) error

	// UninstallApp uninstalls appID from the current device.
	// Whether uninstalling an app that is not installed returns an
	// error or succeeds silently is implementation-specific; a
	// caller that needs to know first should call IsAppInstalled.
	UninstallApp(ctx context.Context, appID string) error

	// ClearAppData resets appID to a clean data state.
	//
	// Platform notes (expand this list when adding support):
	//
	//   - Android: clears data in place via the platform's native
	//     clear primitive. The app stays installed; opts.Source is
	//     ignored.
	//   - iOS: has no direct data-clear primitive reachable from the
	//     host. The adapter uninstalls the app, then reinstalls it
	//     from opts.Source, and returns an error when Source is
	//     empty because there is no way to reach a clean state on
	//     iOS without the artifact to reinstall from.
	ClearAppData(ctx context.Context, appID string, opts ClearAppDataOptions) error

	// IsAppInstalled reports whether appID is currently installed on
	// the device.
	IsAppInstalled(ctx context.Context, appID string) (bool, error)

	// Screenshot captures a PNG-encoded screenshot of the current
	// screen.
	Screenshot(ctx context.Context) ([]byte, error)

	// DeviceInfo reports static information about the device.
	DeviceInfo(ctx context.Context) (DeviceInfo, error)

	// ScreenSize reports the logical screen size in points, not
	// pixels.
	ScreenSize(ctx context.Context) (wire.Size, error)
}
