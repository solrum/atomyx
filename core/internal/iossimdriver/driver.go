package iossimdriver

import (
	"context"
	"errors"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/wire"
)

// Options configures an IosSimDriver.
type Options struct {
	iosdriver.Options

	// HidPort is the WS port the atomyx-sim-hid helper's handshake
	// reported. Required: the device factory must only construct an
	// IosSimDriver after starting the helper and obtaining this port.
	HidPort int

	// HidDispatchTimeout overrides the HID client's dispatch timeout.
	// Zero means defaultDispatchTimeout, the production default; a
	// test shortens this to make a hung HID dispatch fail fast
	// instead of waiting out the production bound.
	HidDispatchTimeout time.Duration
}

// StreamingTouchCapable is the capability interface for drivers that
// accept streaming touch phase events (down, move, up) independent of
// gesture classification. A caller checks for this interface before
// routing streaming input through the HID path instead of
// DispatchGesture.
//
// Only IosSimDriver implements it in this package.
type StreamingTouchCapable interface {
	StreamingTouchDown(ctx context.Context, point wire.Point, id int) error
	StreamingTouchMove(ctx context.Context, point wire.Point, id int) error
	StreamingTouchUp(ctx context.Context, point wire.Point, id int) error
}

// AsStreamingTouchCapable reports whether d also implements
// StreamingTouchCapable, letting a caller route streaming mirror
// input through the HID path when it does.
func AsStreamingTouchCapable(d driver.Driver) (StreamingTouchCapable, bool) {
	s, ok := d.(StreamingTouchCapable)
	return s, ok
}

// IosSimDriver routes touch gestures (Tap, LongPress, Swipe, and the
// StreamingTouchCapable phases) through the atomyx-sim-hid helper's
// HID connection, while every other Driver method delegates to an
// embedded iosdriver.IosDriver (XCUITest-backed). DispatchGesture also
// delegates: multi-pointer gestures require coordinated HID sequencing
// the helper does not expose, so the full gesture goes through
// XCUITest instead.
// cachedScreenSizeTTLMs bounds how long IosSimDriver trusts the
// screen size it cached from the delegate before normalize re-fetches
// it. The simulator can rotate or resize mid-session with no event
// this driver can observe, so caching indefinitely risks converting
// every later point against a swapped width and height; re-fetching
// on every coordinate operation instead would add a delegate
// round trip (an XCUITest call) to every tap, swipe, and streaming
// touch event a script issues. The bound keeps that round trip rare
// while capping how long a stale size can skew a gesture after a
// rotation.
const cachedScreenSizeTTLMs = 3000

type IosSimDriver struct {
	delegate *iosdriver.IosDriver
	hid      *HidClient
	clock    clock.Clock

	// udid names the target device in a failHid message. It is kept
	// separately from delegate rather than read back off it, because
	// the delegate exposes no accessor for its own opts.
	udid string

	// cachedSize is the logical-point screen size used to convert an
	// absolute Point (the Driver contract's coordinate space) into
	// the [0..1]-normalized coordinates the helper expects. Refetched
	// from the delegate whenever unset or older than
	// cachedScreenSizeTTLMs.
	cachedSize    wire.Size
	cachedSizeSet bool
	cachedSizeAt  int64
}

// New constructs an IosSimDriver. conn and proc back the embedded
// IosDriver delegate; dialer backs the HID connection. Both seams
// exist so a test can substitute fakes for the XCUITest transport, the
// process runner, and the HID socket.
func New(conn iosdriver.Conn, proc process.Runner, dialer Dialer, opts Options) (*IosSimDriver, error) {
	if opts.HidPort == 0 {
		return nil, errors.New(
			"iossimdriver: HidPort is required. Start atomyx-sim-hid and pass its handshake port",
		)
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IosSimDriver{
		delegate: iosdriver.New(conn, proc, opts.Options),
		hid:      NewHidClient(dialer, HidClientOptions{Port: opts.HidPort, DispatchTimeout: opts.HidDispatchTimeout}),
		clock:    clk,
		udid:     opts.UDID,
	}, nil
}

var _ driver.Driver = (*IosSimDriver)(nil)
var _ StreamingTouchCapable = (*IosSimDriver)(nil)

func (d *IosSimDriver) Platform() string { return d.delegate.Platform() }

func (d *IosSimDriver) Capabilities() driver.Capabilities { return d.delegate.Capabilities() }

func (d *IosSimDriver) Connect(ctx context.Context) error {
	if err := d.delegate.Connect(ctx); err != nil {
		return err
	}
	return d.failHid(d.hid.Connect(ctx))
}

func (d *IosSimDriver) Disconnect() error {
	_ = d.hid.Dispose()
	return d.delegate.Disconnect()
}

func (d *IosSimDriver) IsConnected() bool { return d.delegate.IsConnected() }

// Ping delegates to the embedded IosDriver: the XCUITest runner
// backs the liveness probe regardless of whether touch dispatch for
// this session routes through it or through the HID helper.
func (d *IosSimDriver) Ping(ctx context.Context) error {
	return d.delegate.Ping(ctx)
}

func (d *IosSimDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	return d.delegate.Hierarchy(ctx)
}

func (d *IosSimDriver) WaitForIdle(ctx context.Context, timeoutMs int) (bool, error) {
	return d.delegate.WaitForIdle(ctx, timeoutMs)
}

// normalize converts an absolute Point in logical points into the
// [0..1]-normalized coordinates the helper expects, dividing by the
// screen size cached from the delegate. The cache is refreshed
// whenever it is unset or older than cachedScreenSizeTTLMs, so a
// simulator rotation or resize is picked up within that bound instead
// of skewing every later coordinate indefinitely. A non-positive
// cached dimension is treated as 1 so a driver still dispatches
// something sane instead of dividing by zero.
func (d *IosSimDriver) normalize(ctx context.Context, point wire.Point) (NormalizedPoint, error) {
	now := d.clock.NowMs()
	if !d.cachedSizeSet || now-d.cachedSizeAt >= cachedScreenSizeTTLMs {
		size, err := d.delegate.ScreenSize(ctx)
		if err != nil {
			return NormalizedPoint{}, err
		}
		d.cachedSize = size
		d.cachedSizeSet = true
		d.cachedSizeAt = now
	}
	w := d.cachedSize.Width
	if w <= 0 {
		w = 1
	}
	h := d.cachedSize.Height
	if h <= 0 {
		h = 1
	}
	return NormalizedPoint{X: point.X / w, Y: point.Y / h}, nil
}

func (d *IosSimDriver) Tap(ctx context.Context, point wire.Point) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	np, err := d.normalize(ctx, point)
	if err != nil {
		return err
	}
	return d.failHid(d.hid.Tap(ctx, np, defaultTapHoldMs))
}

// LongPress holds at point for durationMs. It dispatches the same
// helper tap message as Tap, with durationMs as the hold time, since
// a hold-then-release gesture is what the helper's tap message
// already expresses.
func (d *IosSimDriver) LongPress(ctx context.Context, point wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	np, err := d.normalize(ctx, point)
	if err != nil {
		return err
	}
	return d.failHid(d.hid.Tap(ctx, np, durationMs))
}

func (d *IosSimDriver) Swipe(ctx context.Context, from, to wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	nFrom, err := d.normalize(ctx, from)
	if err != nil {
		return err
	}
	nTo, err := d.normalize(ctx, to)
	if err != nil {
		return err
	}
	return d.failHid(d.hid.Swipe(ctx, nFrom, nTo, durationMs))
}

// DispatchGesture delegates to the embedded IosDriver: multi-pointer
// gestures require coordinated HID sequencing the helper does not
// expose, so the full gesture goes through XCUITest instead. A
// mid-gesture capture waypoint goes through the same delegate call
// and is honored the same way.
func (d *IosSimDriver) DispatchGesture(ctx context.Context, gesture driver.Gesture) (driver.GestureResult, error) {
	return d.delegate.DispatchGesture(ctx, gesture)
}

// StreamingTouchDown dispatches the first phase of a streaming touch
// contact at point. id identifies the contact to the caller; the
// helper's wire protocol carries no separate contact id (see
// HidClient.dispatch).
func (d *IosSimDriver) StreamingTouchDown(ctx context.Context, point wire.Point, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	np, err := d.normalize(ctx, point)
	if err != nil {
		return err
	}
	return d.failHid(d.hid.TouchDown(ctx, np))
}

func (d *IosSimDriver) StreamingTouchMove(ctx context.Context, point wire.Point, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	np, err := d.normalize(ctx, point)
	if err != nil {
		return err
	}
	return d.failHid(d.hid.TouchMove(ctx, np))
}

func (d *IosSimDriver) StreamingTouchUp(ctx context.Context, point wire.Point, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	np, err := d.normalize(ctx, point)
	if err != nil {
		return err
	}
	return d.failHid(d.hid.TouchUp(ctx, np))
}

func (d *IosSimDriver) InputText(ctx context.Context, text string) error {
	return d.delegate.InputText(ctx, text)
}

func (d *IosSimDriver) EraseText(ctx context.Context, count int) error {
	return d.delegate.EraseText(ctx, count)
}

func (d *IosSimDriver) PressKey(ctx context.Context, key driver.KeyCode) (driver.KeyResult, error) {
	return d.delegate.PressKey(ctx, key)
}

func (d *IosSimDriver) HideKeyboard(ctx context.Context) (driver.KeyResult, error) {
	return d.delegate.HideKeyboard(ctx)
}

func (d *IosSimDriver) LaunchApp(ctx context.Context, bundleID string, args driver.LaunchArgs) error {
	return d.delegate.LaunchApp(ctx, bundleID, args)
}

func (d *IosSimDriver) StopApp(ctx context.Context, bundleID string) error {
	return d.delegate.StopApp(ctx, bundleID)
}

func (d *IosSimDriver) KillApp(ctx context.Context, bundleID string) error {
	return d.delegate.KillApp(ctx, bundleID)
}

func (d *IosSimDriver) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	return d.delegate.CurrentForeground(ctx)
}

func (d *IosSimDriver) ListApps(ctx context.Context) ([]driver.InstalledApp, error) {
	return d.delegate.ListApps(ctx)
}

func (d *IosSimDriver) InstallApp(ctx context.Context, source string) error {
	return d.delegate.InstallApp(ctx, source)
}

func (d *IosSimDriver) UninstallApp(ctx context.Context, appID string) error {
	return d.delegate.UninstallApp(ctx, appID)
}

func (d *IosSimDriver) ClearAppData(ctx context.Context, appID string, opts driver.ClearAppDataOptions) error {
	return d.delegate.ClearAppData(ctx, appID, opts)
}

func (d *IosSimDriver) IsAppInstalled(ctx context.Context, appID string) (bool, error) {
	return d.delegate.IsAppInstalled(ctx, appID)
}

func (d *IosSimDriver) Screenshot(ctx context.Context) ([]byte, error) {
	return d.delegate.Screenshot(ctx)
}

func (d *IosSimDriver) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	return d.delegate.DeviceInfo(ctx)
}

func (d *IosSimDriver) ScreenSize(ctx context.Context) (wire.Size, error) {
	return d.delegate.ScreenSize(ctx)
}

// LaunchedApp reports the app the underlying runner driver last
// launched and the launch identifier of the runner it is connected to.
func (d *IosSimDriver) LaunchedApp() (bundleID, runnerID string) {
	return d.delegate.LaunchedApp()
}

// RestoreLaunchedApp makes the underlying runner driver target the app
// an earlier driver had in front. See IosDriver.RestoreLaunchedApp.
func (d *IosSimDriver) RestoreLaunchedApp(ctx context.Context, bundleID, fromRunnerID string) error {
	return d.delegate.RestoreLaunchedApp(ctx, bundleID, fromRunnerID)
}
