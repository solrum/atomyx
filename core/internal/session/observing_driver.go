package session

import (
	"context"
	"sync"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// ObservedDriver is a driver.Driver that counts the mutating calls made
// through it and reports each capture to an Observer.
type ObservedDriver interface {
	driver.Driver
	driver.Unwrapper
	// Epoch reports the action epoch. It advances when a mutating call
	// starts and again when it returns, so a capture taken while an
	// action is in flight never shares an epoch with one taken before
	// or after it.
	Epoch() uint64
}

// NewObservingDriver wraps inner so that every call is forwarded
// unchanged, every mutating call advances the action epoch, and every
// successful Hierarchy, Screenshot and ScreenSize result is reported to
// observer with the epoch that was active when the call began. A capture
// that overlapped a mutating call is reported as unstable, see Observer.
// A nil observer discards the reports.
//
// Errors and results pass through untouched, so errors.Is and errors.As
// see exactly what the inner driver returned.
//
// The result implements driver.StreamingTouchCapable and the
// launched-app accessors of launchedAppCarrier exactly when inner does,
// so a capability check gives the same answer wrapped and unwrapped.
// Each combination is a distinct type that embeds the same core with
// one forwarder per capability, chosen here at construction. A new
// optional interface adds one forwarder and doubles the combinations,
// so prefer a new interface only when it is worth that cost.
//
// Mutating calls are listed explicitly in the driver methods below. A
// driver method added to the port must be classified in the
// observing-driver test before it compiles into a passing suite.
func NewObservingDriver(inner driver.Driver, observer Observer) ObservedDriver {
	if observer == nil {
		observer = noopObserver{}
	}
	core := &observingCore{inner: inner, observer: observer}
	streaming, hasStreaming := inner.(driver.StreamingTouchCapable)
	carrier, hasCarrier := inner.(launchedAppCarrier)
	switch {
	case hasStreaming && hasCarrier:
		return &observedStreamingCarrier{observingCore: core, streamingForwarder: streamingForwarder{core, streaming}, carrierForwarder: carrierForwarder{core, carrier}}
	case hasStreaming:
		return &observedStreaming{observingCore: core, streamingForwarder: streamingForwarder{core, streaming}}
	case hasCarrier:
		return &observedCarrier{observingCore: core, carrierForwarder: carrierForwarder{core, carrier}}
	default:
		return core
	}
}

type observingCore struct {
	inner    driver.Driver
	observer Observer

	mu       sync.Mutex
	epoch    uint64
	inflight int
}

// captureStart is the state of the action epoch as a capture began.
type captureStart struct {
	epoch uint64
	quiet bool
}

var _ ObservedDriver = (*observingCore)(nil)

func (o *observingCore) Unwrap() driver.Driver { return o.inner }

func (o *observingCore) Epoch() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.epoch
}

// mutate opens a mutating call and returns the function that closes it.
func (o *observingCore) mutate() func() {
	o.mu.Lock()
	o.epoch++
	o.inflight++
	o.mu.Unlock()
	return func() {
		o.mu.Lock()
		o.epoch++
		o.inflight--
		o.mu.Unlock()
	}
}

func (o *observingCore) beginCapture() captureStart {
	o.mu.Lock()
	defer o.mu.Unlock()
	return captureStart{epoch: o.epoch, quiet: o.inflight == 0}
}

// stable reports whether no mutating call overlapped the capture that
// began at start. A call already in flight at the start fails it, and
// so does any call that started or finished since, because either one
// moves the epoch.
func (o *observingCore) stable(start captureStart) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return start.quiet && o.epoch == start.epoch
}

func (o *observingCore) Platform() string                  { return o.inner.Platform() }
func (o *observingCore) Capabilities() driver.Capabilities { return o.inner.Capabilities() }
func (o *observingCore) Connect(ctx context.Context) error { return o.inner.Connect(ctx) }
func (o *observingCore) Disconnect() error                 { return o.inner.Disconnect() }
func (o *observingCore) IsConnected() bool                 { return o.inner.IsConnected() }
func (o *observingCore) Ping(ctx context.Context) error    { return o.inner.Ping(ctx) }
func (o *observingCore) AgentInfo() driver.AgentInfo       { return o.inner.AgentInfo() }

func (o *observingCore) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	start := o.beginCapture()
	tree, err := o.inner.Hierarchy(ctx)
	if err == nil {
		o.observer.ObserveTree(tree, start.epoch, o.stable(start))
	}
	return tree, err
}

func (o *observingCore) Screenshot(ctx context.Context) ([]byte, error) {
	start := o.beginCapture()
	image, err := o.inner.Screenshot(ctx)
	if err == nil {
		o.observer.ObserveScreenshot(image, start.epoch, o.stable(start))
	}
	return image, err
}

func (o *observingCore) ScreenSize(ctx context.Context) (wire.Size, error) {
	start := o.beginCapture()
	size, err := o.inner.ScreenSize(ctx)
	if err == nil {
		o.observer.ObserveScreenSize(size, start.epoch, o.stable(start))
	}
	return size, err
}

func (o *observingCore) WaitForIdle(ctx context.Context, timeoutMs int) (bool, error) {
	return o.inner.WaitForIdle(ctx, timeoutMs)
}

func (o *observingCore) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	return o.inner.CurrentForeground(ctx)
}

func (o *observingCore) ListApps(ctx context.Context) ([]driver.InstalledApp, error) {
	return o.inner.ListApps(ctx)
}

func (o *observingCore) IsAppInstalled(ctx context.Context, appID string) (bool, error) {
	return o.inner.IsAppInstalled(ctx, appID)
}

func (o *observingCore) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	return o.inner.DeviceInfo(ctx)
}

func (o *observingCore) Tap(ctx context.Context, point wire.Point) error {
	defer o.mutate()()
	return o.inner.Tap(ctx, point)
}

func (o *observingCore) LongPress(ctx context.Context, point wire.Point, durationMs int) error {
	defer o.mutate()()
	return o.inner.LongPress(ctx, point, durationMs)
}

func (o *observingCore) Swipe(ctx context.Context, from wire.Point, to wire.Point, durationMs int) error {
	defer o.mutate()()
	return o.inner.Swipe(ctx, from, to, durationMs)
}

func (o *observingCore) DispatchGesture(ctx context.Context, gesture driver.Gesture) (driver.GestureResult, error) {
	defer o.mutate()()
	return o.inner.DispatchGesture(ctx, gesture)
}

func (o *observingCore) InputText(ctx context.Context, text string) error {
	defer o.mutate()()
	return o.inner.InputText(ctx, text)
}

func (o *observingCore) EraseText(ctx context.Context, count int) error {
	defer o.mutate()()
	return o.inner.EraseText(ctx, count)
}

func (o *observingCore) PressKey(ctx context.Context, key driver.KeyCode) (driver.KeyResult, error) {
	defer o.mutate()()
	return o.inner.PressKey(ctx, key)
}

func (o *observingCore) HideKeyboard(ctx context.Context) (driver.KeyResult, error) {
	defer o.mutate()()
	return o.inner.HideKeyboard(ctx)
}

func (o *observingCore) LaunchApp(ctx context.Context, bundleID string, args driver.LaunchArgs) error {
	defer o.mutate()()
	return o.inner.LaunchApp(ctx, bundleID, args)
}

func (o *observingCore) StopApp(ctx context.Context, bundleID string) error {
	defer o.mutate()()
	return o.inner.StopApp(ctx, bundleID)
}

func (o *observingCore) KillApp(ctx context.Context, bundleID string) error {
	defer o.mutate()()
	return o.inner.KillApp(ctx, bundleID)
}

func (o *observingCore) InstallApp(ctx context.Context, source string) error {
	defer o.mutate()()
	return o.inner.InstallApp(ctx, source)
}

func (o *observingCore) UninstallApp(ctx context.Context, appID string) error {
	defer o.mutate()()
	return o.inner.UninstallApp(ctx, appID)
}

func (o *observingCore) OpenLink(ctx context.Context, link string, opts driver.OpenLinkOptions) error {
	defer o.mutate()()
	return o.inner.OpenLink(ctx, link, opts)
}

func (o *observingCore) ClearAppData(ctx context.Context, appID string, opts driver.ClearAppDataOptions) error {
	defer o.mutate()()
	return o.inner.ClearAppData(ctx, appID, opts)
}
