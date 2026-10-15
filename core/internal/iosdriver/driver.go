package iosdriver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/version"
	"dev.atomyx/internal/wire"
)

const defaultIosPort = 22087

// runnerBusyRemedy is the next step reported when the agent accepts a
// connection but does not answer because another client holds it.
const runnerBusyRemedy = "Close the other Atomyx session or client that is using the device, or restart the runner, then try again."

// pingProbeTimeout bounds a single Ping round trip to the runner, so
// a caller gets a fast answer instead of waiting on the request's
// full context deadline.
const pingProbeTimeout = 3 * time.Second

// Kind classifies whether an IosDriver targets a simulator or a
// physical device. The two share the same TCP command surface;
// Kind only decides which transport-startup and app-lifecycle
// commands the adapter issues.
type Kind string

const (
	KindSimulator Kind = "simulator"
	KindDevice    Kind = "device"
)

// Options configures an IosDriver.
type Options struct {
	// UDID is the simulator or device UDID. Required for Kind ==
	// KindDevice, where it is passed to iproxy; informational only
	// for Kind == KindSimulator, where simctl commands that need a
	// UDID use it directly.
	UDID string

	// Kind selects the simulator or physical-device command surface.
	Kind Kind

	// Port is the host-side TCP port the Swift driver listens on.
	// Defaults to 22087. Set a distinct value per driver when more
	// than one iOS driver runs concurrently.
	Port int

	// Clock is the time source launchIproxy polls and times out
	// against while waiting for a freshly spawned USB tunnel to become
	// reachable. Defaults to clock.SystemClock{} when nil, so only a
	// test that wants deterministic timing over IproxyErrTunnelTimeout
	// needs to set it.
	Clock clock.Clock

	// Background starts the long-lived iproxy tunnel process. It is
	// required: New panics when it is nil, because a default would
	// spawn real processes from a caller that never asked for them.
	Background process.BackgroundRunner

	// RunnerID is the launch identifier the caller assigned to the
	// runner it expects on Port. Empty means the runner was not
	// launched by the caller, so identity rests on the UDID alone.
	RunnerID string

	// Warn receives a one-line warning when a runner is adopted
	// without a launch identifier. Nil discards it.
	Warn func(message string)

	// ExternalTunnel states that something else owns the USB tunnel to
	// a physical device, already running on Port, so Connect does not
	// start an iproxy of its own. A driver built without a provisioner
	// leaves it false and starts and stops its own tunnel.
	ExternalTunnel bool

	// ExternalRunner states that the caller owns the runner on Port and
	// this driver did not launch it. Connect then confirms the answering
	// runner belongs to UDID, because the runner can be replaced between
	// the caller's own check and the connection. A runner that reports
	// no identity, or one that belongs to another target, fails Connect
	// with a *RunnerIdentityError.
	ExternalRunner bool

	// OwnRunnerBundleID and OwnRunnerAppName identify the Atomyx
	// runner, so the foreign automation probe never reports it. The
	// bundle id is matched against simulator job labels and the app name
	// against physical device executables. Empty values exclude
	// nothing.
	OwnRunnerBundleID string
	OwnRunnerAppName  string
}

// IosDriver implements driver.Driver over the Swift XCUITest runner's
// TCP command surface.
//
// Transport topology:
//
//   - Simulator: the host reaches 127.0.0.1:<port> directly, because
//     the simulator shares the host's network namespace.
//   - Physical device: the runner listens on 127.0.0.1:<port> inside
//     the device's process; an iproxy USB tunnel maps the same port
//     on the host to that device-side listener before the TCP client
//     connects.
//
// What this driver deliberately does not do: selector resolution,
// scroll-into-view, and obscurement detection. Those stay in the
// composition layer above the Driver interface, operating on the
// canonical wire.TreeNodeWire this adapter emits from Hierarchy.
type IosDriver struct {
	opts Options
	conn Conn
	proc process.Runner
	// background starts processes that outlive a single command, so
	// they are not subject to Runner.Run's fallback timeout.
	background process.BackgroundRunner

	capabilities         driver.Capabilities
	lastLaunchedBundleID string
	// connectedRunnerID is the launch identifier the runner reported in
	// the ping that completed Connect. Empty when the runner reports none.
	connectedRunnerID string

	iproxyStop func()

	// agentInfo is the version pair the runner reported at Connect.
	// It stays zero until the runner reports versions.
	agentInfo driver.AgentInfo

	// foreign holds the foreign automation sessions the probe started by
	// the last Connect found. foreignMu guards it together with the probe
	// bookkeeping, because health checks read it while the probe goroutine
	// and a reconnect write it. foreignGeneration identifies the current
	// probe so a superseded one never publishes, foreignCancel stops it,
	// and foreignWG lets a test wait for it to finish.
	foreignMu         sync.Mutex
	foreign           []driver.ForeignAutomationSession
	foreignGeneration uint64
	foreignCancel     context.CancelFunc
	foreignWG         sync.WaitGroup
}

// New constructs an IosDriver bound to opts.UDID. conn and proc are
// injected so a test can substitute fakes for both the TCP command
// channel and the xcrun or iproxy command line.
func New(conn Conn, proc process.Runner, opts Options) *IosDriver {
	if opts.Port == 0 {
		opts.Port = defaultIosPort
	}
	if opts.Clock == nil {
		opts.Clock = clock.SystemClock{}
	}
	background := opts.Background
	if background == nil {
		panic("iosdriver.New: Options.Background is required")
	}
	return &IosDriver{
		opts:       opts,
		conn:       conn,
		proc:       proc,
		background: background,
		capabilities: driver.Capabilities{
			CanScreenshot:      true,
			CanEraseText:       true,
			CanWaitForIdle:     false,
			CanSetLocation:     false,
			CanSetOrientation:  false,
			CanHideKeyboard:    true,
			CanQueryForeground: false,
			CanMultiPointer:    false,
			CanPressure:        false,
			SupportedKeyCodes:  []driver.KeyCode{driver.KeyBack, driver.KeyHome, driver.KeyEnter},
		},
	}
}

var _ driver.Driver = (*IosDriver)(nil)

func (d *IosDriver) Platform() string { return "ios" }

func (d *IosDriver) Capabilities() driver.Capabilities { return d.capabilities }

type pingCapabilities struct {
	CanMultiPointer bool `json:"canMultiPointer"`
	CanPressure     bool `json:"canPressure"`
}

type pingResponse struct {
	Capabilities pingCapabilities `json:"capabilities"`
	RunnerID     string           `json:"runnerId"`
	// TargetUDID and SimulatorUDID are the target identities the runner
	// reports, empty when it reports none.
	TargetUDID    string `json:"targetUdid"`
	SimulatorUDID string `json:"simulatorUdid"`
	// ProtocolVersion is zero when the runner reports none.
	ProtocolVersion int    `json:"protocolVersion"`
	AgentVersion    string `json:"agentVersion"`
}

// Connect opens the transport and verifies the runner is live via a
// ping handshake, which also populates the gesture capabilities
// (CanMultiPointer, CanPressure) the active dispatch backend
// supports. On a physical device it first launches an iproxy USB
// tunnel through the injected Runner and waits for that tunnel to
// become reachable, because the runner is unreachable from the host
// loopback until that tunnel is up; launchIproxy returns an
// *IproxyError, without ever reaching the dial below, when the
// tunnel fails to come up within its budget.
//
// Connect always clears lastLaunchedBundleID first, even on a
// reconnect of a driver that was never explicitly Disconnected: the
// runner process behind conn may have restarted (or the connection may
// now reach a different runner instance entirely) since any prior
// launch this driver tracked, so trusting a stale bundle id would let
// LaunchApp's NoReset attach fast path skip dispatch for an app that
// is not actually running.
//
// A Connect that fails closes the transport and stops the iproxy tunnel
// this driver started, so no failure path leaves a tunnel process behind.
// A tunnel that was already running and was reused is never stopped.
//
// Connect starts a background probe for foreign UI automation sessions
// and does not wait for it, so a slow device listing never delays the
// connection. ForeignAutomation reports the result once the probe ends.
func (d *IosDriver) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.lastLaunchedBundleID = ""
	d.connectedRunnerID = ""
	d.agentInfo = driver.AgentInfo{}
	connected := false
	defer func() {
		if !connected {
			_ = d.conn.Close()
			d.stopIproxy()
		}
	}()
	if d.opts.Kind == KindDevice && !d.opts.ExternalTunnel {
		if err := d.launchIproxy(ctx); err != nil {
			var iproxyErr *IproxyError
			if errors.As(err, &iproxyErr) && iproxyErr.Code == IproxyErrRunnerBusy {
				return diag.New(diag.CodeAgentNotReady,
					fmt.Sprintf("the agent on device %s did not answer, it may be busy with another client or not be an Atomyx runner", d.opts.UDID),
					runnerBusyRemedy,
					err)
			}
			return diag.New(diag.CodeAgentUnreachable,
				fmt.Sprintf("could not reach the agent on device %s", d.opts.UDID),
				"Confirm the device is connected and unlocked, then try again.",
				err)
		}
	}
	if err := d.conn.Connect(ctx); err != nil {
		return diag.New(diag.CodeAgentUnreachable,
			fmt.Sprintf("could not reach the agent on device %s", d.opts.UDID),
			"Confirm the agent is running on the device, then try again.",
			fmt.Errorf("iosdriver: connect: %w", err))
	}
	var pong pingResponse
	if err := d.conn.CallInto(ctx, "ping", map[string]any{}, &pong); err != nil {
		return d.fail(err)
	}
	if d.opts.ExternalRunner {
		if idErr := d.verifyExternalRunner(pong); idErr != nil {
			return idErr
		}
	}
	if pong.ProtocolVersion != version.AgentProtocolVersion {
		return connectProtocolMismatchError(pong.ProtocolVersion)
	}
	d.agentInfo = driver.AgentInfo{ProtocolVersion: pong.ProtocolVersion, AgentVersion: pong.AgentVersion}
	d.capabilities.CanMultiPointer = pong.Capabilities.CanMultiPointer
	d.capabilities.CanPressure = pong.Capabilities.CanPressure
	d.connectedRunnerID = pong.RunnerID
	connected = true
	d.startForeignAutomationProbe(ctx)
	return nil
}

// verifyExternalRunner applies the identity rule to the ping reply of a
// runner the caller owns. A nil result means the runner is acceptable.
func (d *IosDriver) verifyExternalRunner(pong pingResponse) *RunnerIdentityError {
	want := ExpectedRunner{UDID: d.opts.UDID, Kind: d.opts.Kind, RunnerID: d.opts.RunnerID}
	got := RunnerIdentity{
		RunnerID:        pong.RunnerID,
		TargetUDID:      pong.TargetUDID,
		SimulatorUDID:   pong.SimulatorUDID,
		ProtocolVersion: pong.ProtocolVersion,
		AgentVersion:    pong.AgentVersion,
	}
	verdict, reason := want.Verify(got)
	if verdict == IdentityMismatch || verdict == IdentityLegacy {
		return &RunnerIdentityError{Port: d.opts.Port, Expect: want, Got: got, Reason: reason, Legacy: verdict == IdentityLegacy}
	}
	return nil
}

// LaunchedApp reports the bundle id of the app this driver last
// launched or saw in front, and the launch identifier of the runner
// the driver is connected to. Either value is empty when unknown.
func (d *IosDriver) LaunchedApp() (bundleID, runnerID string) {
	return d.lastLaunchedBundleID, d.connectedRunnerID
}

// RestoreLaunchedApp makes this driver target the app an earlier driver
// had in front, so a tool call that needs an app works again without
// the caller launching it.
//
// When the runner this driver connected to is the same instance that
// launched the app, which the runner's launch identifier confirms, the
// runner already targets it and only the tracked bundle id is seeded.
// Any other runner, including one that reports no identifier, has no
// target app, because a runner can query only an app it launched
// itself, so the app is launched through this runner. That launch does
// not reset an instance the runner already tracks, but a runner that
// tracks none starts the app fresh, which discards in-memory app state.
//
// An empty bundleID restores nothing and reports no error.
func (d *IosDriver) RestoreLaunchedApp(ctx context.Context, bundleID, fromRunnerID string) error {
	if bundleID == "" {
		return nil
	}
	if fromRunnerID != "" && d.connectedRunnerID == fromRunnerID {
		d.lastLaunchedBundleID = bundleID
		return nil
	}
	noReset := true
	return d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &noReset})
}

// Disconnect closes the transport, stops the iproxy tunnel if one was
// launched, cancels a foreign automation probe still running, and clears
// launch-tracked state. It never stops a runner,
// which outlives the connection. It is idempotent: the underlying
// Conn.Close and the iproxy stop are both safe to call when nothing is
// running.
func (d *IosDriver) Disconnect() error {
	d.stopForeignAutomationProbe()
	_ = d.conn.Close()
	d.stopIproxy()
	d.lastLaunchedBundleID = ""
	return nil
}

func (d *IosDriver) IsConnected() bool { return d.conn.IsConnected() }

// AgentInfo returns the versions the runner reported at Connect. It
// never contacts the device.
func (d *IosDriver) AgentInfo() driver.AgentInfo { return d.agentInfo }

// Ping reuses the same "ping" round trip Connect's handshake performs,
// bounded to pingProbeTimeout so a caller gets a fast answer instead
// of waiting on the request's full context deadline.
func (d *IosDriver) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingProbeTimeout)
	defer cancel()
	var pong pingResponse
	if err := d.conn.CallInto(pingCtx, "ping", map[string]any{}, &pong); err != nil {
		return diag.New(diag.CodeAgentUnreachable,
			fmt.Sprintf("could not reach the agent on device %s", d.opts.UDID),
			"Confirm the agent is running on the device, then try again.",
			fmt.Errorf("iosdriver: ping: %w", err))
	}
	return nil
}

// listRunningUIKitBundleIDsPattern extracts the bundle id slice from
// an "xcrun simctl spawn <udid> launchctl list" output line, which
// reports each launched UIKit application as a label of the form
// "UIKitApplication:<bundleId>[<pid>][...".
var listRunningUIKitBundleIDsPattern = regexp.MustCompile(`UIKitApplication:([^\s\[]+)`)

// Hierarchy captures the current UI snapshot via dumpRawTree. When no
// app has been launched through this driver yet and it targets a
// simulator, it first queries currently running UIKit bundle ids via
// the injected Runner and passes them as candidates, so the runner
// can pick whichever is in the foreground without an explicit
// LaunchApp round-trip. That query is best effort: a failure yields
// no candidates rather than surfacing an error, since Hierarchy can
// still proceed on the runner's own fallback path.
func (d *IosDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	if err := ctx.Err(); err != nil {
		return wire.TreeNodeWire{}, err
	}
	args := map[string]any{}
	if d.lastLaunchedBundleID == "" && d.opts.Kind == KindSimulator {
		if candidates := d.listRunningUIKitBundleIDs(ctx); len(candidates) > 0 {
			args["bundleIdCandidates"] = candidates
		}
	}
	var resp dumpRawTreeResponse
	if err := d.conn.CallInto(ctx, "dumpRawTree", args, &resp); err != nil {
		return wire.TreeNodeWire{}, d.fail(err)
	}
	if resp.BundleID != "" {
		d.lastLaunchedBundleID = resp.BundleID
	}
	root := resp.Root
	if root == nil {
		root = &IosRawElement{ElementType: stringPtr("other")}
	}
	return NormalizeIosTree(*root), nil
}

func (d *IosDriver) listRunningUIKitBundleIDs(ctx context.Context) []string {
	result, err := d.proc.Run(ctx, "xcrun", "simctl", "spawn", d.opts.UDID, "launchctl", "list")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		m := listRunningUIKitBundleIDsPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		seen[m[1]] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// WaitForIdle is a defensive no-op: Capabilities reports
// CanWaitForIdle false, so a caller must route around it with
// tree-diff polling instead, but a misbehaving caller that invokes it
// anyway gets a truthy return rather than a hang.
func (d *IosDriver) WaitForIdle(ctx context.Context, timeoutMs int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *IosDriver) Tap(ctx context.Context, point wire.Point) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.fail(d.conn.CallInto(ctx, "tapAt", map[string]any{"x": point.X, "y": point.Y}, nil))
}

func (d *IosDriver) LongPress(ctx context.Context, point wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.fail(d.conn.CallInto(ctx, "longPressAt", map[string]any{
		"x": point.X, "y": point.Y, "durationMs": durationMs,
	}, nil))
}

func (d *IosDriver) Swipe(ctx context.Context, from, to wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.fail(d.conn.CallInto(ctx, "swipe", map[string]any{
		"fromX": from.X, "fromY": from.Y, "toX": to.X, "toY": to.Y, "durationMs": durationMs,
	}, nil))
}

// dispatchPointerResponse is the dispatchPointer wire reply.
// CapturedImages is present only when the dispatched gesture carried
// at least one GesturePhaseCapture waypoint; a plain gesture gets
// back a response with an empty or absent field.
type dispatchPointerResponse struct {
	CapturedImages []struct {
		Base64   string `json:"base64"`
		MimeType string `json:"mimeType"`
	} `json:"capturedImages"`
}

// DispatchGesture rejects a gesture the runner's active backend
// cannot dispatch before it ever reaches the runner: more than one
// pointer when Capabilities().CanMultiPointer is false, or a
// pressure-varying waypoint when Capabilities().CanPressure is
// false. A mid-gesture GesturePhaseCapture waypoint is forwarded to
// the runner, which takes the screenshot while the touch is held and
// returns it; the decoded image is appended to the result in the
// same order the waypoints were dispatched.
func (d *IosDriver) DispatchGesture(ctx context.Context, gesture driver.Gesture) (driver.GestureResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.GestureResult{}, err
	}
	if err := driver.ValidateGesture(gesture); err != nil {
		return driver.GestureResult{}, err
	}
	if len(gesture.Pointers) > 1 && !d.capabilities.CanMultiPointer {
		return driver.GestureResult{}, fmt.Errorf(
			"iOS driver cannot dispatch a %d-pointer gesture: multi-pointer capability is unavailable on this Xcode or iOS combination",
			len(gesture.Pointers),
		)
	}
	for _, p := range gesture.Pointers {
		for _, w := range p.Waypoints {
			if w.Pressure != nil && !d.capabilities.CanPressure {
				return driver.GestureResult{}, errors.New(
					"iOS driver cannot dispatch pressure waypoints: this runner build does not support per-touch pressure",
				)
			}
		}
	}

	pointers := make([]map[string]any, len(gesture.Pointers))
	for i, p := range gesture.Pointers {
		waypoints := make([]map[string]any, len(p.Waypoints))
		for j, w := range p.Waypoints {
			wp := map[string]any{
				"phase":           string(w.Phase),
				"x":               w.Point.X,
				"y":               w.Point.Y,
				"atOffsetSeconds": w.AtOffsetSeconds,
			}
			if w.Pressure != nil {
				wp["pressure"] = *w.Pressure
			}
			waypoints[j] = wp
		}
		pointers[i] = map[string]any{"id": p.ID, "waypoints": waypoints}
	}
	var resp dispatchPointerResponse
	if err := d.conn.CallInto(ctx, "dispatchPointer", map[string]any{"pointers": pointers}, &resp); err != nil {
		return driver.GestureResult{}, d.fail(err)
	}
	if len(resp.CapturedImages) == 0 {
		return driver.GestureResult{}, nil
	}
	images := make([]driver.CapturedImage, len(resp.CapturedImages))
	for i, img := range resp.CapturedImages {
		data, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			return driver.GestureResult{}, d.fail(fmt.Errorf("iosdriver: decode captured image %d: %w", i, err))
		}
		images[i] = driver.CapturedImage{Data: data, MimeType: img.MimeType}
	}
	return driver.GestureResult{CapturedImages: images}, nil
}

type typeTextResponse struct {
	Success *bool  `json:"success"`
	Reason  string `json:"reason"`
}

// InputText types into the focused field. A runner reply with success
// false means nothing was typed, which surfaces as an agent-rejected
// error carrying the runner's reason. A reply without the field counts
// as success.
func (d *IosDriver) InputText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var resp typeTextResponse
	if err := d.conn.CallInto(ctx, "typeText", map[string]any{"text": text}, &resp); err != nil {
		return d.fail(err)
	}
	if resp.Success != nil && !*resp.Success {
		return diag.New(diag.CodeAgentRejected,
			"nothing was typed because no input field has keyboard focus",
			"Tap the input field so it has focus, then type again.",
			nil)
	}
	return nil
}

func (d *IosDriver) EraseText(ctx context.Context, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.fail(d.conn.CallInto(ctx, "clearFocusedInput", map[string]any{"maxDeletes": count}, nil))
}

type pressKeyResponse struct {
	AffordanceFound bool   `json:"affordanceFound"`
	Strategy        string `json:"strategy"`
}

// PressKey reports the runner's affordanceFound flag as KeyResult.Ok:
// true means a verifiable control was used (nav bar back, a home
// device press, enter into a focused field); false means a
// best-effort gesture was dispatched with no confirmable effect (for
// example the iOS back-gesture fallback). Strategy is surfaced as
// Reason for observability.
func (d *IosDriver) PressKey(ctx context.Context, key driver.KeyCode) (driver.KeyResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.KeyResult{}, err
	}
	var resp pressKeyResponse
	if err := d.conn.CallInto(ctx, "pressKey", map[string]any{"key": string(key)}, &resp); err != nil {
		return driver.KeyResult{}, d.fail(err)
	}
	return driver.KeyResult{Ok: resp.AffordanceFound, Reason: resp.Strategy}, nil
}

type hideKeyboardResponse struct {
	Ok       bool   `json:"ok"`
	Strategy string `json:"strategy"`
}

func (d *IosDriver) HideKeyboard(ctx context.Context) (driver.KeyResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.KeyResult{}, err
	}
	var resp hideKeyboardResponse
	if err := d.conn.CallInto(ctx, "hideKeyboard", map[string]any{}, &resp); err != nil {
		return driver.KeyResult{}, d.fail(err)
	}
	return driver.KeyResult{Ok: resp.Ok, Reason: resp.Strategy}, nil
}

// LaunchApp dispatches per the tri-state of args.NoReset:
//
//   - nil (plain launch): dispatched unconditionally to the runner,
//     which launches via XCUIApplication.launch(). No pre-check, no
//     explicit stop.
//   - false (fresh): stops bundleID first, then dispatches a clean
//     launch.
//   - true (attach): if bundleID is already the last-launched app,
//     the call is skipped and the running instance is reused.
//     Otherwise it falls back to the same plain dispatch as the
//     other cases, since there is no tracked instance to attach to.
func (d *IosDriver) LaunchApp(ctx context.Context, bundleID string, args driver.LaunchArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case args.NoReset != nil && *args.NoReset:
		if d.lastLaunchedBundleID == bundleID {
			return nil
		}
	case args.NoReset != nil && !*args.NoReset:
		if err := d.StopApp(ctx, bundleID); err != nil {
			return d.fail(err)
		}
	}
	noReset := args.NoReset != nil && *args.NoReset
	if err := d.conn.CallInto(ctx, "launchApp", map[string]any{"bundleId": bundleID, "noReset": noReset}, nil); err != nil {
		return d.fail(err)
	}
	d.lastLaunchedBundleID = bundleID
	return nil
}

func (d *IosDriver) StopApp(ctx context.Context, bundleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.conn.CallInto(ctx, "forceStopApp", map[string]any{"bundleId": bundleID}, nil); err != nil {
		return d.fail(err)
	}
	if d.lastLaunchedBundleID == bundleID {
		d.lastLaunchedBundleID = ""
	}
	return nil
}

// KillApp forces the same route as StopApp: the XCUITest layer draws
// no distinction between a graceful stop and a forced kill.
func (d *IosDriver) KillApp(ctx context.Context, bundleID string) error {
	return d.StopApp(ctx, bundleID)
}

// CurrentForeground always returns the typed unsupported error:
// querying the device for the true foreground app would require
// private APIs this driver does not use, and reporting the bundle id
// this driver last launched would be a fabricated answer once the
// user or the system has switched the foreground app since. See
// Capabilities().CanQueryForeground, which reports false for this
// driver.
func (d *IosDriver) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	if err := ctx.Err(); err != nil {
		return driver.ForegroundInfo{}, err
	}
	return driver.ForegroundInfo{}, diag.New(diag.CodeForegroundUnsupported,
		"the current foreground app cannot be determined on this device",
		"Avoid relying on the current foreground app on this device.",
		nil)
}

func (d *IosDriver) Screenshot(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var resp struct {
		Base64 string `json:"base64"`
	}
	if err := d.conn.CallInto(ctx, "screenshot", map[string]any{}, &resp); err != nil {
		return nil, d.fail(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.Base64)
	if err != nil {
		return nil, d.fail(fmt.Errorf("iosdriver: decode screenshot: %w", err))
	}
	return decoded, nil
}

// DeviceInfo reports a minimal descriptor: PlatformVersion and Model
// are always "unknown", because the runner exposes no device-info
// command; only UDID and Kind carry real information.
func (d *IosDriver) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	if err := ctx.Err(); err != nil {
		return driver.DeviceInfo{}, err
	}
	kind := driver.DeviceKindDevice
	if d.opts.Kind == KindSimulator {
		kind = driver.DeviceKindSimulator
	}
	return driver.DeviceInfo{
		Platform:        "ios",
		PlatformVersion: "unknown",
		Model:           "unknown",
		UDID:            d.opts.UDID,
		Kind:            kind,
	}, nil
}

func (d *IosDriver) ScreenSize(ctx context.Context) (wire.Size, error) {
	if err := ctx.Err(); err != nil {
		return wire.Size{}, err
	}
	var resp struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := d.conn.CallInto(ctx, "getScreenSize", map[string]any{}, &resp); err != nil {
		return wire.Size{}, d.fail(err)
	}
	return wire.Size{Width: resp.Width, Height: resp.Height}, nil
}
