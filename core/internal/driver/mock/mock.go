package mock

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// Driver is an in-memory driver.Driver implementation for tests. It
// is scripted rather than simulated: a test stages a queue of
// hierarchy snapshots, one per expected Hierarchy call, and Driver
// returns them in order. When the queue is empty, the last-staged
// tree is returned repeatedly (sticky final state). Gesture and
// lifecycle calls are recorded in Calls, in dispatch order, so a
// test can assert the right sequence was dispatched.
//
// Driver is not safe for concurrent use from multiple goroutines. A
// caller that shares one Driver across goroutines must serialize its
// own access.
type Driver struct {
	capabilities driver.Capabilities
	connected    bool

	hierarchyQueue []wire.TreeNodeWire
	lastHierarchy  *wire.TreeNodeWire

	// HangOnNextHierarchy, when true, makes the next Hierarchy call
	// block until ctx is done, returning ctx.Err() instead of a
	// staged tree. It resets to false after that call. A test uses
	// it to model a hung driver and confirm that cancellation
	// propagates end to end; the caller must pass a context that is
	// eventually canceled, or the call blocks forever.
	HangOnNextHierarchy bool

	foreignAutomation []driver.ForeignAutomationSession

	// Screen is the screen size ScreenSize returns.
	Screen wire.Size

	// Calls records every gesture and lifecycle call in dispatch
	// order, keyed by the Driver method name (for example "Tap",
	// "LaunchApp").
	Calls []Call

	// RunningApp is the bundle or package id Driver currently
	// considers foreground. LaunchApp, StopApp, and KillApp update
	// it; a test may set it directly to seed an "already running"
	// state before calling LaunchApp.
	RunningApp *string

	// OnSwipe, when set, is invoked after every Swipe call so a test
	// can react to it, for example by advancing the hierarchy queue
	// to a post-scroll tree.
	OnSwipe func(from, to wire.Point)

	// OnHideKeyboard, when set, is invoked on every HideKeyboard
	// call. A non-nil return becomes the HideKeyboard result; a nil
	// return keeps the default {Ok: true}.
	OnHideKeyboard func() *driver.KeyResult

	// InstalledApps is the set of app ids Driver currently considers
	// installed. InstallApp adds the string it was called with,
	// UninstallApp removes it, IsAppInstalled checks membership. A
	// test that cares about the distinction between a source path
	// and an app id passes the same string to both, or seeds this
	// set directly.
	InstalledApps map[string]struct{}

	// ClearedApps records the app ids passed to ClearAppData, in
	// call order.
	ClearedApps []string

	// Apps is the list ListApps returns. Empty (not nil) by default,
	// matching a device that reports zero installed apps rather than
	// an enumeration failure.
	Apps []driver.InstalledApp

	// ScreenshotData is the payload Screenshot returns, defaulting to
	// an empty slice. A test asserting on the captured payload itself
	// (for example a check for a PNG-encoded result) sets this field
	// first.
	ScreenshotData []byte

	// OpenedLinks records the links passed to OpenLink, in call order.
	OpenedLinks []string

	// ScreenshotErr, SwipeErr, PressKeyErr, LaunchAppErr, ListAppsErr,
	// OpenLinkErr, and PingErr, when set, make the matching method return that
	// error instead of its normal result, so a test can drive a
	// caller's error-surfacing path without a real device.
	ScreenshotErr error
	SwipeErr      error
	PressKeyErr   error
	LaunchAppErr  error
	ListAppsErr   error
	OpenLinkErr   error
	PingErr       error

	// Agent is the AgentInfo the mock reports, settable so a test can
	// drive a caller's version-reporting path without a real agent.
	Agent driver.AgentInfo
}

// Call is one recorded gesture or lifecycle call.
type Call struct {
	Method string
	Args   []any
}

// New constructs a Driver with the default capability set: screen
// capture, text erase, keyboard dismissal, and foreground query
// supported; idle wait, location, orientation, multi-pointer, and
// pressure not supported; back, home, and enter as the supported key
// codes.
func New() *Driver {
	return &Driver{
		capabilities: driver.Capabilities{
			CanScreenshot:      true,
			CanEraseText:       true,
			CanWaitForIdle:     false,
			CanSetLocation:     false,
			CanSetOrientation:  false,
			CanHideKeyboard:    true,
			CanOpenLink:        true,
			CanQueryForeground: true,
			CanMultiPointer:    false,
			CanPressure:        false,
			SupportedKeyCodes:  []driver.KeyCode{driver.KeyBack, driver.KeyHome, driver.KeyEnter},
		},
		Screen:        wire.Size{Width: 430, Height: 932},
		InstalledApps: make(map[string]struct{}),
	}
}

var _ driver.Driver = (*Driver)(nil)

// StageHierarchy queues tree to be returned from the next Hierarchy
// call. Chain multiple calls to model a scripted sequence of
// states.
func (d *Driver) StageHierarchy(tree wire.TreeNodeWire) *Driver {
	d.hierarchyQueue = append(d.hierarchyQueue, tree)
	return d
}

// StageHierarchyForContract queues tree the same way StageHierarchy
// does, without the chain-friendly return value. It implements the
// driver contract suite's Stager interface, letting that suite
// preload a hierarchy without depending on Driver's concrete type.
func (d *Driver) StageHierarchyForContract(tree wire.TreeNodeWire) {
	d.StageHierarchy(tree)
}

// StageHierarchyRepeated queues tree for the next n Hierarchy
// calls.
func (d *Driver) StageHierarchyRepeated(tree wire.TreeNodeWire, n int) *Driver {
	for i := 0; i < n; i++ {
		d.hierarchyQueue = append(d.hierarchyQueue, tree)
	}
	return d
}

// ClearCalls resets recorded calls without touching staged
// hierarchies.
func (d *Driver) ClearCalls() {
	d.Calls = nil
}

// SetCapabilities overrides the capability flags Driver reports, for
// a test exercising capability-gated fallback behavior.
func (d *Driver) SetCapabilities(c driver.Capabilities) {
	d.capabilities = c
}

func (d *Driver) Platform() string { return "mock" }

func (d *Driver) Capabilities() driver.Capabilities { return d.capabilities }

func (d *Driver) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.connected = true
	return nil
}

func (d *Driver) Disconnect() error {
	d.connected = false
	return nil
}

func (d *Driver) IsConnected() bool { return d.connected }

// Ping reports the mock agent healthy unless PingErr is set, matching
// the same error-injection pattern as Screenshot and PressKey.
func (d *Driver) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.PingErr != nil {
		return d.PingErr
	}
	d.record("Ping")
	return nil
}

// AgentInfo returns the Agent field and records no call, matching the
// port contract that it never reaches a device.
func (d *Driver) AgentInfo() driver.AgentInfo { return d.Agent }

// SetForeignAutomation stages the sessions ForeignAutomation reports,
// replacing any staged earlier. A nil or empty list reports none.
func (d *Driver) SetForeignAutomation(sessions ...driver.ForeignAutomationSession) {
	d.foreignAutomation = append([]driver.ForeignAutomationSession(nil), sessions...)
}

// ForeignAutomation returns a copy of the staged sessions and records
// no call, matching the port contract that it never reaches a device.
func (d *Driver) ForeignAutomation() []driver.ForeignAutomationSession {
	if len(d.foreignAutomation) == 0 {
		return nil
	}
	return append([]driver.ForeignAutomationSession(nil), d.foreignAutomation...)
}

var _ driver.ForeignAutomationReporter = (*Driver)(nil)

// FailNextPingForContract sets err as the error Ping returns until a
// test clears PingErr. A driver contract suite caller wires this as
// its drivertest.Hooks.FailPing, letting that suite force an
// unreachable result without a test-injection method on a production
// driver type.
func (d *Driver) FailNextPingForContract(err error) {
	d.PingErr = err
}

func (d *Driver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	if err := ctx.Err(); err != nil {
		return wire.TreeNodeWire{}, err
	}
	d.record("Hierarchy")
	if d.HangOnNextHierarchy {
		d.HangOnNextHierarchy = false
		<-ctx.Done()
		return wire.TreeNodeWire{}, ctx.Err()
	}
	if len(d.hierarchyQueue) > 0 {
		tree := d.hierarchyQueue[0]
		d.hierarchyQueue = d.hierarchyQueue[1:]
		d.lastHierarchy = &tree
		return tree, nil
	}
	if d.lastHierarchy != nil {
		return *d.lastHierarchy, nil
	}
	return wire.TreeNodeWire{}, errors.New("mock.Driver.Hierarchy: no tree staged, call StageHierarchy first")
}

func (d *Driver) WaitForIdle(ctx context.Context, timeoutMs int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	d.record("WaitForIdle", timeoutMs)
	return true, nil
}

func (d *Driver) Tap(ctx context.Context, point wire.Point) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("Tap", point)
	return nil
}

func (d *Driver) LongPress(ctx context.Context, point wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("LongPress", point, durationMs)
	return nil
}

func (d *Driver) Swipe(ctx context.Context, from wire.Point, to wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.SwipeErr != nil {
		return d.SwipeErr
	}
	d.record("Swipe", from, to, durationMs)
	if d.OnSwipe != nil {
		d.OnSwipe(from, to)
	}
	return nil
}

// DispatchGesture rejects a gesture the reported capabilities do not
// support, matching the rejection every real adapter performs before
// dispatch: more than one pointer when CanMultiPointer is false, or a
// pressure-varying waypoint when CanPressure is false. A test that
// wants to exercise the accepted branch sets those capabilities true
// via SetCapabilities first.
//
// Driver supports GesturePhaseCapture unconditionally: every capture
// waypoint in gesture yields one synthetic, decodable image in the
// returned GestureResult, in dispatch order, so a test can exercise
// the full mid-gesture-capture path with no real device.
func (d *Driver) DispatchGesture(ctx context.Context, gesture driver.Gesture) (driver.GestureResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.GestureResult{}, err
	}
	if err := driver.ValidateGesture(gesture); err != nil {
		return driver.GestureResult{}, err
	}
	if len(gesture.Pointers) > 1 && !d.capabilities.CanMultiPointer {
		return driver.GestureResult{}, fmt.Errorf(
			"mock.Driver cannot dispatch a %d-pointer gesture: CanMultiPointer is false",
			len(gesture.Pointers),
		)
	}
	var images []driver.CapturedImage
	for _, p := range gesture.Pointers {
		for _, w := range p.Waypoints {
			if w.Pressure != nil && !d.capabilities.CanPressure {
				return driver.GestureResult{}, errors.New("mock.Driver cannot dispatch pressure waypoints: CanPressure is false")
			}
			if w.Phase == driver.GesturePhaseCapture {
				images = append(images, driver.CapturedImage{Data: syntheticCaptureImagePNG, MimeType: "image/png"})
			}
		}
	}
	d.record("DispatchGesture", gesture)
	return driver.GestureResult{CapturedImages: images}, nil
}

func (d *Driver) InputText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("InputText", text)
	return nil
}

// EraseText rejects the call when the reported capabilities report
// CanEraseText false, matching a driver whose caller is expected to
// check the capability first: nothing higher up this stack routes an
// EraseText call through when the flag is false, so a driver that
// still receives one is honoring a contract violation, not a
// legitimate request. A test that wants the accepted branch leaves
// CanEraseText at its true default or sets it explicitly.
func (d *Driver) EraseText(ctx context.Context, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.capabilities.CanEraseText {
		return errors.New("mock.Driver.EraseText: CanEraseText is false")
	}
	d.record("EraseText", count)
	return nil
}

// PressKey rejects key when it is absent from the reported
// SupportedKeyCodes, exercising the driver-specific-key branch the
// KeyCode contract allows: a value outside the named constants is
// accepted by an adapter only at its own discretion, and a mock
// configured with a fixed SupportedKeyCodes list declines the rest so
// a test can drive a caller's rejection path without a real device.
func (d *Driver) PressKey(ctx context.Context, key driver.KeyCode) (driver.KeyResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.KeyResult{}, err
	}
	if d.PressKeyErr != nil {
		return driver.KeyResult{}, d.PressKeyErr
	}
	if !supportsKeyCode(d.capabilities.SupportedKeyCodes, key) {
		return driver.KeyResult{}, fmt.Errorf("mock.Driver.PressKey: %q is not in SupportedKeyCodes", key)
	}
	d.record("PressKey", key)
	return driver.KeyResult{Ok: true}, nil
}

func supportsKeyCode(supported []driver.KeyCode, key driver.KeyCode) bool {
	for _, k := range supported {
		if k == key {
			return true
		}
	}
	return false
}

// HideKeyboard rejects the call when the reported capabilities report
// CanHideKeyboard false, the same caller-must-check contract EraseText
// honors. A test that wants the accepted branch leaves CanHideKeyboard
// at its true default or sets it explicitly.
func (d *Driver) HideKeyboard(ctx context.Context) (driver.KeyResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.KeyResult{}, err
	}
	if !d.capabilities.CanHideKeyboard {
		return driver.KeyResult{}, errors.New("mock.Driver.HideKeyboard: CanHideKeyboard is false")
	}
	d.record("HideKeyboard")
	if d.OnHideKeyboard != nil {
		if result := d.OnHideKeyboard(); result != nil {
			return *result, nil
		}
	}
	return driver.KeyResult{Ok: true}, nil
}

// OpenLink validates link, then records it in OpenedLinks. It fails
// with diag.CodeLinkUnsupported when the reported capabilities report
// CanOpenLink false, the same caller-must-check contract the real
// adapters honor.
func (d *Driver) OpenLink(ctx context.Context, link string, opts driver.OpenLinkOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.capabilities.CanOpenLink {
		return diag.NewLinkUnsupported("")
	}
	if err := driver.ValidateLink(link); err != nil {
		return err
	}
	d.record("OpenLink", link, opts.AppID)
	if d.OpenLinkErr != nil {
		return d.OpenLinkErr
	}
	d.OpenedLinks = append(d.OpenedLinks, link)
	return nil
}

func (d *Driver) LaunchApp(ctx context.Context, bundleID string, args driver.LaunchArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.LaunchAppErr != nil {
		return d.LaunchAppErr
	}
	if args.NoReset != nil && *args.NoReset {
		foreground, err := d.CurrentForeground(ctx)
		if err != nil {
			return err
		}
		if foreground.BundleID != nil && *foreground.BundleID == bundleID {
			return nil
		}
	} else if args.NoReset != nil && !*args.NoReset {
		if err := d.StopApp(ctx, bundleID); err != nil {
			return err
		}
	}
	d.record("LaunchApp", bundleID, args)
	id := bundleID
	d.RunningApp = &id
	return nil
}

func (d *Driver) StopApp(ctx context.Context, bundleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("StopApp", bundleID)
	if d.RunningApp != nil && *d.RunningApp == bundleID {
		d.RunningApp = nil
	}
	return nil
}

func (d *Driver) KillApp(ctx context.Context, bundleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("KillApp", bundleID)
	if d.RunningApp != nil && *d.RunningApp == bundleID {
		d.RunningApp = nil
	}
	return nil
}

func (d *Driver) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	if err := ctx.Err(); err != nil {
		return driver.ForegroundInfo{}, err
	}
	return driver.ForegroundInfo{BundleID: d.RunningApp}, nil
}

func (d *Driver) ListApps(ctx context.Context) ([]driver.InstalledApp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.ListAppsErr != nil {
		return nil, d.ListAppsErr
	}
	if d.Apps == nil {
		return []driver.InstalledApp{}, nil
	}
	return d.Apps, nil
}

func (d *Driver) InstallApp(ctx context.Context, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("InstallApp", source)
	d.InstalledApps[source] = struct{}{}
	return nil
}

func (d *Driver) UninstallApp(ctx context.Context, appID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("UninstallApp", appID)
	delete(d.InstalledApps, appID)
	return nil
}

func (d *Driver) ClearAppData(ctx context.Context, appID string, opts driver.ClearAppDataOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.record("ClearAppData", appID, opts.Source)
	d.ClearedApps = append(d.ClearedApps, appID)
	return nil
}

func (d *Driver) IsAppInstalled(ctx context.Context, appID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	d.record("IsAppInstalled", appID)
	_, ok := d.InstalledApps[appID]
	return ok, nil
}

func (d *Driver) Screenshot(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.ScreenshotErr != nil {
		return nil, d.ScreenshotErr
	}
	if d.ScreenshotData != nil {
		return d.ScreenshotData, nil
	}
	return []byte{}, nil
}

func (d *Driver) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	if err := ctx.Err(); err != nil {
		return driver.DeviceInfo{}, err
	}
	return driver.DeviceInfo{
		Platform:        "mock",
		PlatformVersion: "0.0.0",
		Model:           "Mock",
		UDID:            "mock-udid",
		Kind:            driver.DeviceKindSimulator,
	}, nil
}

func (d *Driver) ScreenSize(ctx context.Context) (wire.Size, error) {
	if err := ctx.Err(); err != nil {
		return wire.Size{}, err
	}
	return d.Screen, nil
}

func (d *Driver) record(method string, args ...any) {
	d.Calls = append(d.Calls, Call{Method: method, Args: args})
}
