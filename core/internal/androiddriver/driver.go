package androiddriver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/version"
	"dev.atomyx/internal/wire"
)

// gestureRequestTimeout bounds a single dispatchGesture call. The
// agent waits on its own gesture-completion callback, which in turn
// waits for the gesture to fully replay; this is set wider than that
// internal budget so a legitimate long gesture is reported through
// the agent's own structured timeout response rather than a generic
// transport timeout racing it.
const gestureRequestTimeout = 35 * time.Second

const defaultAndroidPort = 8765

// Options configures an AndroidDriver.
type Options struct {
	// Serial is the Android device serial from "adb devices".
	Serial string

	// HostPort is the host-side TCP port forwarded to the device.
	// Defaults to 8765 when zero. Ignored when AllocateHostPort is
	// set.
	HostPort int

	// AllocateHostPort asks adb to pick a free host port for this
	// device ("adb forward tcp:0 <devicePort>") instead of using
	// HostPort. Connect reuses a forward that already maps this serial
	// to DevicePort, and otherwise creates one and records the port adb
	// printed. This is the setting to use whenever more than one
	// Android driver may run on the workstation at the same time.
	// Requires NewTransport, because the transport can only be built
	// once the port is known.
	AllocateHostPort bool

	// NewTransport builds the transport for the host port Connect
	// settled on. Required when AllocateHostPort is set, and ignored
	// otherwise.
	NewTransport func(hostPort int) Transport

	// OwnAnsweringForward makes Connect take ownership of a forward it
	// reused when the agent answered /health through it, so Disconnect
	// removes it. Set it only when the caller holds the device
	// exclusively, because no other process then relies on that forward.
	// A forward whose agent did not answer is never owned. Applies only
	// with AllocateHostPort.
	OwnAnsweringForward bool

	// DevicePort is the device-side port the agent's HTTP server
	// listens on. Defaults to 8765.
	DevicePort int

	// ReinstallAgent replaces the agent on the device and brings it up
	// again. Connect calls it once when the running agent is too old to
	// mark secret fields, and it must report an *AgentLauncherError with
	// ErrCodeNoAPK when no agent APK can be found. Nil means the driver
	// cannot reinstall, so Connect fails for such an agent instead.
	ReinstallAgent func(ctx context.Context) error
}

// AndroidDriver implements driver.Driver over the Android agent's
// HTTP control server, reachable through an adb-forwarded TCP tunnel.
//
// Capability flags reported by Capabilities are conservative until
// Connect completes: canMultiPointer and canPressure only flip to
// what the agent's active gesture backend supports once the /ping
// handshake in Connect succeeds.
type AndroidDriver struct {
	opts      Options
	transport Transport
	proc      process.Runner

	connected    bool
	capabilities driver.Capabilities

	// forwardOwnedByThis reports whether this Connect call created the
	// adb forward this driver is using, as opposed to reusing one that
	// already existed for this serial and port pair. Only a forward
	// this driver created is safe for Disconnect to remove: a caller,
	// a prior run, or another driver instance may still depend on a
	// forward it did not create.
	forwardOwnedByThis bool

	// hostPort is the host-side port the live forward uses: the fixed
	// option value, or the port adb allocated for this serial.
	hostPort int

	// agentInfo is the version pair the agent reported at Connect.
	// It stays zero until the agent reports versions.
	agentInfo driver.AgentInfo
}

// New constructs an AndroidDriver bound to opts.Serial. t and proc
// are injected so a test can substitute fakes for both the HTTP
// control server and the adb command line.
func New(t Transport, proc process.Runner, opts Options) *AndroidDriver {
	if opts.HostPort == 0 {
		opts.HostPort = defaultAndroidPort
	}
	if opts.DevicePort == 0 {
		opts.DevicePort = defaultAndroidPort
	}
	return &AndroidDriver{
		hostPort:  opts.HostPort,
		opts:      opts,
		transport: t,
		proc:      proc,
		capabilities: driver.Capabilities{
			CanScreenshot:      true,
			CanEraseText:       true,
			CanWaitForIdle:     false,
			CanSetLocation:     false,
			CanSetOrientation:  false,
			CanHideKeyboard:    true,
			CanQueryForeground: true,
			CanMultiPointer:    false,
			CanPressure:        false,
			SupportedKeyCodes:  []driver.KeyCode{driver.KeyBack, driver.KeyHome, driver.KeyEnter},
		},
	}
}

var _ driver.Driver = (*AndroidDriver)(nil)

func (d *AndroidDriver) Platform() string { return "android" }

func (d *AndroidDriver) Capabilities() driver.Capabilities { return d.capabilities }

// Connect first probes for a foreign automation session already
// driving the device, then ensures the adb TCP tunnel is in place,
// verifies the agent answers /health, and runs the /ping capability
// handshake.
//
// The /ping outcome decides the result:
//
//   - The route cannot be reached at all (a transport failure): the
//     handshake proves nothing about the agent, so Connect succeeds
//     with the conservative default capabilities and a zero
//     AgentInfo. An agent build that predates the /ping route must
//     still be usable at the single-pointer, no-pressure gesture
//     surface it always supported.
//   - The agent answers with an error status, a protocol version other
//     than the host's, or without the secret field capability: the
//     agent is stale. It is replaced once through Options.ReinstallAgent
//     and pinged again. Connect fails with a diagnostic when it cannot
//     be replaced, or when the replacement is still stale or no longer
//     answers /ping.
//
// AgentInfo is the zero value after a Connect that fails, and after
// one that succeeds without a /ping answer.
//
// Connect checks "adb forward --list" before creating a forward,
// because a host-side local port is a real listening socket shared by
// the whole workstation, not a per-driver resource. Three outcomes
// follow:
//
//   - No forward holds the local port: Connect creates one and
//     remembers that this driver owns it, so Disconnect later removes
//     it.
//   - A forward already holds the local port, pointing at this same
//     serial and device port: Connect reuses it without recreating
//     it, and does not mark it as owned. The forward may have been
//     left behind by a prior run, set up manually by an operator, or
//     created by another driver instance targeting the same device;
//     in every case, removing it out from under whoever still needs
//     it would be a regression, so only the driver that actually
//     creates a forward may remove it.
//   - A forward already holds the local port, but points somewhere
//     else (a different serial, or the same serial forwarded to a
//     different device port): the local port is not this driver's to
//     take. Silently replacing it could sever a session another tool
//     or another Atomyx driver instance is actively using, so Connect
//     fails with a diagnostic naming the conflict rather than
//     guessing which side should win.
func (d *AndroidDriver) Connect(ctx context.Context) error {
	d.agentInfo = driver.AgentInfo{}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkForForeignAutomation(ctx, d.proc, d.opts.Serial); err != nil {
		return err
	}

	remotePort := fmt.Sprintf("tcp:%d", d.opts.DevicePort)

	forwards, err := listAdbForwards(ctx, d.proc)
	if err != nil {
		return d.unreachable(fmt.Errorf("androiddriver: adb forward --list: %w", err))
	}

	if d.opts.AllocateHostPort {
		return d.connectAllocated(ctx, forwards, remotePort)
	}

	localPort := fmt.Sprintf("tcp:%d", d.opts.HostPort)
	if existing, found := findAdbForward(forwards, localPort); found {
		if existing.serial != d.opts.Serial || existing.remote != remotePort {
			return diag.New(diag.CodeHostPortInUse,
				fmt.Sprintf("could not reach the agent on device %s: its forwarded port is already claimed by a different device", d.opts.Serial),
				"Remove the conflicting adb forward for the other device, then try again.",
				nil)
		}
		d.forwardOwnedByThis = false
	} else {
		if _, err := d.proc.Run(ctx, "adb",
			"-s", d.opts.Serial, "forward", localPort, remotePort,
		); err != nil {
			return d.unreachable(fmt.Errorf("androiddriver: adb forward: %w", err))
		}
		d.forwardOwnedByThis = true
	}
	d.hostPort = d.opts.HostPort
	return d.finishConnect(ctx)
}

// connectAllocated is the AllocateHostPort branch of Connect. It
// reuses a live forward that maps this serial to the device port,
// and otherwise lets adb allocate a fresh host port. A reused forward
// whose agent does not answer at the transport level is treated as
// stale, which is what adb kill-server or a device reboot leaves
// behind, so it is removed and a fresh forward is allocated once.
func (d *AndroidDriver) connectAllocated(ctx context.Context, forwards []adbForward, remotePort string) error {
	if d.opts.NewTransport == nil {
		return d.unreachable(errors.New("androiddriver: AllocateHostPort requires Options.NewTransport"))
	}

	if port, found := findSerialForward(forwards, d.opts.Serial, remotePort); found {
		d.hostPort = port
		d.forwardOwnedByThis = false
		d.transport = d.opts.NewTransport(port)
		err := d.finishConnect(ctx)
		if err == nil {
			d.forwardOwnedByThis = d.opts.OwnAnsweringForward
			return nil
		}
		if !isStaleForwardFailure(err) {
			return err
		}
		_, _ = d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "forward", "--remove", fmt.Sprintf("tcp:%d", port))
	}

	result, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "forward", "tcp:0", remotePort)
	if err != nil {
		return d.unreachable(fmt.Errorf("androiddriver: adb forward tcp:0: %w", err))
	}
	port, err := parseAllocatedPort(result.Stdout)
	if err != nil {
		return d.unreachable(err)
	}
	d.hostPort = port
	d.forwardOwnedByThis = true
	d.transport = d.opts.NewTransport(port)
	return d.finishConnect(ctx)
}

// finishConnect verifies the agent answers /health and runs the /ping
// capability handshake against the forward Connect settled on. An
// agent too old to mark secret fields is replaced once through
// Options.ReinstallAgent, and Connect fails when it cannot be.
func (d *AndroidDriver) finishConnect(ctx context.Context) error {
	if _, err := d.transport.Health(ctx); err != nil {
		return d.classifyHealthFailure(fmt.Errorf("androiddriver: health check: %w", err))
	}
	pong, err := d.transport.Ping(ctx)
	if agentIsStale(pong, err) {
		if err := d.replaceStaleAgent(ctx, pong, err); err != nil {
			return err
		}
		pong, err = d.transport.Ping(ctx)
		if err != nil || agentIsStale(pong, err) {
			return d.staleAgentError(pong, err, nil)
		}
	}
	if err == nil {
		d.agentInfo = driver.AgentInfo{ProtocolVersion: pong.ProtocolVersion, AgentVersion: pong.AgentVersion}
		d.capabilities.CanMultiPointer = pong.Capabilities.CanMultiPointer
		d.capabilities.CanPressure = pong.Capabilities.CanPressure
	}
	d.connected = true
	return nil
}

// agentLacksSecureFields reports whether the /ping outcome shows an
// agent that does not mark secret fields: it answered without the
// capability, or it answered with an error status because it predates
// the route. A failure to reach the route at all proves nothing about
// the agent, so it keeps the conservative defaults instead.
func agentLacksSecureFields(pong transport.PingResponse, err error) bool {
	if err == nil {
		return !pong.Capabilities.SecureFields
	}
	var agentErr *transport.AgentError
	return errors.As(err, &agentErr)
}

// agentProtocolMismatch reports the protocol version the /ping outcome
// shows and whether it differs from the one this host supports. An
// agent that answered with an error status predates the route and
// counts as version 0. A failure to reach the route at all proves
// nothing about the agent, so it is not a mismatch.
func agentProtocolMismatch(pong transport.PingResponse, err error) (agentVersion int, mismatch bool) {
	agentVersion, known := PingedProtocolVersion(pong, err)
	return agentVersion, known && agentVersion != version.AgentProtocolVersion
}

// PingedProtocolVersion reports the protocol version a /ping outcome
// shows and whether it shows one. An agent that answered with an error
// status predates the route and counts as version 0. A failure to reach
// the route at all shows nothing, so known is false.
func PingedProtocolVersion(pong transport.PingResponse, err error) (agentVersion int, known bool) {
	if err == nil {
		return pong.ProtocolVersion, true
	}
	var agentErr *transport.AgentError
	return 0, errors.As(err, &agentErr)
}

// agentIsStale reports whether the agent must be replaced: its
// protocol version differs from the host's, or it does not mark secret
// fields. The secure fields check stays beside the version check
// because password masking depends on it even when the versions agree.
func agentIsStale(pong transport.PingResponse, err error) bool {
	if _, mismatch := agentProtocolMismatch(pong, err); mismatch {
		return true
	}
	return agentLacksSecureFields(pong, err)
}

func (d *AndroidDriver) replaceStaleAgent(ctx context.Context, pong transport.PingResponse, pingErr error) error {
	if d.opts.ReinstallAgent == nil {
		return d.staleAgentError(pong, pingErr, nil)
	}
	if err := d.opts.ReinstallAgent(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return d.staleAgentError(pong, pingErr, err)
	}
	return nil
}

// staleAgentRemedy is the next step reported for an agent that is out
// of date. Connecting to it could leave a password unmasked or speak a
// wire contract the host does not, so the connection is refused instead.
const staleAgentRemedy = "Build the agent with ./gradlew :app:assembleDebug in platforms/android-agent, " +
	"or set ATOMYX_ANDROID_APK to a current agent APK, then connect again so the agent is reinstalled."

// staleAgentError reports why the agent behind the /ping outcome cannot
// be used: AGENT_VERSION_MISMATCH when its protocol version differs
// from the host's, AGENT_NOT_READY when only the secret field marker
// is missing.
func (d *AndroidDriver) staleAgentError(pong transport.PingResponse, pingErr, cause error) error {
	if agentVersion, mismatch := agentProtocolMismatch(pong, pingErr); mismatch {
		_, message, _ := diag.Describe(diag.NewAgentVersionMismatch(agentVersion, version.AgentProtocolVersion))
		return diag.New(diag.CodeAgentVersionMismatch, message, staleAgentRemedy, cause)
	}
	return diag.New(diag.CodeAgentNotReady,
		fmt.Sprintf("the agent on device %s is out of date and cannot mark secret fields", d.opts.Serial),
		staleAgentRemedy,
		cause)
}

// isStaleForwardFailure reports whether err is a health failure where
// the agent never answered, as opposed to an agent that answered and
// reported itself not ready.
func isStaleForwardFailure(err error) bool {
	code, _, _ := diag.Describe(err)
	return code == diag.CodeAgentUnreachable
}

func (d *AndroidDriver) unreachable(cause error) error {
	return diag.New(diag.CodeAgentUnreachable,
		fmt.Sprintf("could not reach the agent on device %s", d.opts.Serial),
		"Confirm the device is connected and unlocked, then try again.",
		cause)
}

// Disconnect is DisconnectContext with no bound on the forward removal.
func (d *AndroidDriver) Disconnect() error { return d.DisconnectContext(context.Background()) }

// DisconnectContext removes the adb forward only when this Connect call
// created it. A forward Connect found already in place stays,
// because it was not this driver's to take in the first place, and
// some other caller, run, or driver instance may still be relying on
// it. Removal is best-effort: a failure to remove it (for example the
// forward was already gone) does not surface as an error, matching
// Driver.Disconnect's idempotent-and-never-throws contract. The removal
// runs under ctx and is skipped once ctx is done, because the device may
// already belong to another process whose forward on the same port a late
// removal would take away.
func (d *AndroidDriver) DisconnectContext(ctx context.Context) error {
	if !d.connected {
		return nil
	}
	if d.forwardOwnedByThis {
		if ctx.Err() == nil {
			_, _ = d.proc.Run(ctx, "adb",
				"-s", d.opts.Serial, "forward", "--remove",
				fmt.Sprintf("tcp:%d", d.hostPort),
			)
		}
	}
	d.connected = false
	d.forwardOwnedByThis = false
	return nil
}

func (d *AndroidDriver) IsConnected() bool { return d.connected }

// AgentInfo returns the versions the agent reported at Connect. It
// never contacts the device.
func (d *AndroidDriver) AgentInfo() driver.AgentInfo { return d.agentInfo }

// Ping reuses the same /health route Connect verifies against,
// bounded to healthProbeTimeout so a caller gets a fast answer
// instead of waiting on the request's full context deadline.
func (d *AndroidDriver) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pingCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	if _, err := d.transport.Health(pingCtx); err != nil {
		return d.classifyHealthFailure(fmt.Errorf("androiddriver: ping: %w", err))
	}
	return nil
}

// agentNotReadyRemedy is the actionable next step reported when the
// agent answered /health but declared itself unable to serve
// requests, as opposed to not answering at all.
const agentNotReadyRemedy = "Re-enable the accessibility service on the device, or run `atomyx setup --platform android` to find what is missing, then try again."

// classifyHealthFailure turns a failed /health call into a diag error,
// distinguishing an agent that never answered from one that answered
// and reported itself not ready. err is expected to wrap the error
// transport.HTTPClient.Health returned, so a *transport.AgentError
// reached by unwrapping it means the agent responded with its own
// not-ready status rather than the connection failing outright.
func (d *AndroidDriver) classifyHealthFailure(err error) error {
	var agentErr *transport.AgentError
	if errors.As(err, &agentErr) {
		return diag.New(diag.CodeAgentNotReady,
			diag.SanitizeAgentMessage(agentErr.Message),
			agentNotReadyRemedy,
			err)
	}
	return diag.New(diag.CodeAgentUnreachable,
		fmt.Sprintf("could not reach the agent on device %s", d.opts.Serial),
		"Confirm the agent is running on the device, then try again.",
		err)
}

func (d *AndroidDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	if err := ctx.Err(); err != nil {
		return wire.TreeNodeWire{}, err
	}
	raw, err := d.transport.Tree(ctx)
	if err != nil {
		return wire.TreeNodeWire{}, d.fail(err)
	}
	return NormalizeAndroidTree(raw), nil
}

// WaitForIdle is a defensive no-op: Capabilities reports
// CanWaitForIdle false, so a caller must route around it with
// tree-diff polling instead, but a misbehaving caller that invokes it
// anyway gets a truthy return rather than a hang.
func (d *AndroidDriver) WaitForIdle(ctx context.Context, timeoutMs int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *AndroidDriver) Tap(ctx context.Context, point wire.Point) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := d.transport.TapCoords(ctx, transport.TapCoordsRequest{X: point.X, Y: point.Y})
	return d.fail(err)
}

func (d *AndroidDriver) LongPress(ctx context.Context, point wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := d.transport.LongPress(ctx, transport.LongPressRequest{
		X: point.X, Y: point.Y, DurationMs: int64(durationMs),
	})
	return d.fail(err)
}

func (d *AndroidDriver) Swipe(ctx context.Context, from, to wire.Point, durationMs int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := d.transport.Swipe(ctx, transport.SwipeRequest{
		FromX: from.X, FromY: from.Y, ToX: to.X, ToY: to.Y, DurationMs: int64(durationMs),
	})
	return d.fail(err)
}

// DispatchGesture rejects a gesture the agent's active backend cannot
// dispatch before it ever reaches the agent: more than one pointer
// when Capabilities().CanMultiPointer is false, or a pressure-varying
// waypoint when Capabilities().CanPressure is false. A mid-gesture
// GesturePhaseCapture waypoint is forwarded to the agent, which takes
// a screenshot while the touch is held and returns it; the decoded
// image is appended to the result in the same order the waypoints
// were dispatched.
func (d *AndroidDriver) DispatchGesture(ctx context.Context, gesture driver.Gesture) (driver.GestureResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.GestureResult{}, err
	}
	if len(gesture.Pointers) == 0 {
		return driver.GestureResult{}, errors.New("dispatchGesture: empty pointers array")
	}
	if len(gesture.Pointers) > 1 && !d.capabilities.CanMultiPointer {
		return driver.GestureResult{}, fmt.Errorf(
			"android driver cannot dispatch a %d-pointer gesture: multi-pointer dispatch is unavailable on this agent build",
			len(gesture.Pointers),
		)
	}
	for _, p := range gesture.Pointers {
		for _, w := range p.Waypoints {
			if w.Pressure != nil && !d.capabilities.CanPressure {
				return driver.GestureResult{}, errors.New(
					"android driver cannot dispatch pressure waypoints: this agent build does not support per-touch pressure",
				)
			}
		}
	}

	pointers := make([]transport.GesturePointer, len(gesture.Pointers))
	for i, p := range gesture.Pointers {
		waypoints := make([]transport.GestureWaypoint, len(p.Waypoints))
		for j, w := range p.Waypoints {
			waypoints[j] = transport.GestureWaypoint{
				Phase:           string(w.Phase),
				X:               w.Point.X,
				Y:               w.Point.Y,
				AtOffsetSeconds: w.AtOffsetSeconds,
				Pressure:        w.Pressure,
			}
		}
		pointers[i] = transport.GesturePointer{ID: p.ID, Waypoints: waypoints}
	}

	gctx, cancel := context.WithTimeout(ctx, gestureRequestTimeout)
	defer cancel()
	resp, err := d.transport.DispatchGesture(gctx, transport.DispatchGestureRequest{Pointers: pointers})
	if err != nil {
		return driver.GestureResult{}, d.fail(err)
	}
	if len(resp.CapturedImages) == 0 {
		return driver.GestureResult{}, nil
	}
	images := make([]driver.CapturedImage, len(resp.CapturedImages))
	for i, img := range resp.CapturedImages {
		data, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			return driver.GestureResult{}, d.fail(fmt.Errorf("androiddriver: decode captured image %d: %w", i, err))
		}
		images[i] = driver.CapturedImage{Data: data, MimeType: img.MimeType}
	}
	return driver.GestureResult{CapturedImages: images}, nil
}

// InputText appends to whatever field currently has focus. The agent
// defaults ClearFirst to true when the field is absent from the
// request, so it is sent explicitly false here to honor the
// Driver.InputText append contract; a caller that wants to clear
// first drives EraseText as its own step.
func (d *AndroidDriver) InputText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clearFirst := false
	_, err := d.transport.TypeKeyboard(ctx, transport.TypeKeyboardRequest{Text: text, ClearFirst: &clearFirst})
	return d.fail(err)
}

// EraseText ignores count: the agent's clear-focused-input route
// always clears the field's entire current value in one call. On
// failure it returns an AGENT_REJECTED error carrying the sanitized
// agent reason, wrapping a *ClearTextError with the agent's full
// diagnosis, not just the reason string, so a caller can distinguish
// a clear failure from a transport error and report why it happened.
func (d *AndroidDriver) EraseText(ctx context.Context, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	res, err := d.transport.ClearFocusedInput(ctx)
	if err != nil {
		return d.fail(err)
	}
	if res.Ok {
		return nil
	}
	strategiesTried := res.StrategiesTried
	if strategiesTried == nil {
		strategiesTried = []string{}
	}
	focusedNodeDesc := res.FocusedNodeDesc
	if focusedNodeDesc == "" {
		focusedNodeDesc = "unknown"
	}
	return d.fail(&ClearTextError{
		Reason: res.Reason,
		Diagnostic: ClearTextDiagnostic{
			StrategiesTried: strategiesTried,
			LastValue:       res.LastValue,
			FocusedNodeDesc: focusedNodeDesc,
			ScreenWidth:     res.ScreenWidth,
			ScreenHeight:    res.ScreenHeight,
		},
	})
}

// PressKey reports Ok unconditionally: Android system key dispatch
// always fires regardless of whether the foreground app handled it,
// so there is no verifiable affordance to report against.
func (d *AndroidDriver) PressKey(ctx context.Context, key driver.KeyCode) (driver.KeyResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.KeyResult{}, err
	}
	if _, err := d.transport.PressKey(ctx, transport.KeyRequest{Key: string(key)}); err != nil {
		return driver.KeyResult{}, d.fail(err)
	}
	return driver.KeyResult{Ok: true}, nil
}

func (d *AndroidDriver) HideKeyboard(ctx context.Context) (driver.KeyResult, error) {
	if err := ctx.Err(); err != nil {
		return driver.KeyResult{}, err
	}
	res, err := d.transport.HideKeyboard(ctx)
	if err != nil {
		return driver.KeyResult{}, d.fail(err)
	}
	return driver.KeyResult{Ok: res.Ok, Reason: res.Reason}, nil
}

// LaunchApp launches bundleID. When args.NoReset is explicitly false,
// it force-stops the package first so the launch starts from a clean
// process; if that stop is refused, LaunchApp returns the
// *ForceStopError from StopApp instead of relaunching, since relaunching
// on top of a process that never actually stopped would leave the
// app's prior state in place while reporting a clean reset.
func (d *AndroidDriver) LaunchApp(ctx context.Context, bundleID string, args driver.LaunchArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case args.NoReset != nil && *args.NoReset:
		foreground, err := d.CurrentForeground(ctx)
		if err != nil {
			return d.fail(err)
		}
		if foreground.BundleID != nil && *foreground.BundleID == bundleID {
			return nil
		}
	case args.NoReset != nil && !*args.NoReset:
		if err := d.StopApp(ctx, bundleID); err != nil {
			return d.fail(err)
		}
	}
	_, err := d.transport.LaunchApp(ctx, transport.LaunchRequest{PackageName: bundleID})
	return d.fail(err)
}

// StopApp force-stops bundleID. It returns a *ForceStopError, not
// nil, when the agent reports the stop was refused: an ok:false
// response here means the package's process is still alive, so a
// caller must treat this the same as a stop that never happened.
func (d *AndroidDriver) StopApp(ctx context.Context, bundleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	res, err := d.transport.ForceStopApp(ctx, transport.ForceStopRequest{PackageName: bundleID})
	if err != nil {
		return d.fail(err)
	}
	if !res.Ok {
		return d.fail(&ForceStopError{PackageName: bundleID, Reason: res.Reason})
	}
	return nil
}

// KillApp forces the same route as StopApp: the agent's
// AccessibilityService surface draws no distinction between a
// graceful stop and a forced kill.
func (d *AndroidDriver) KillApp(ctx context.Context, bundleID string) error {
	return d.StopApp(ctx, bundleID)
}

func (d *AndroidDriver) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	if err := ctx.Err(); err != nil {
		return driver.ForegroundInfo{}, err
	}
	raw, err := d.transport.CurrentActivity(ctx)
	if err != nil {
		return driver.ForegroundInfo{}, d.fail(err)
	}
	var bundleID *string
	if raw.PackageName != "" {
		bundleID = &raw.PackageName
	}
	return driver.ForegroundInfo{BundleID: bundleID, Activity: raw.Activity}, nil
}

func (d *AndroidDriver) ListApps(ctx context.Context) ([]driver.InstalledApp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := d.transport.Apps(ctx)
	if err != nil {
		return nil, d.fail(err)
	}
	out := make([]driver.InstalledApp, len(raw))
	for i, a := range raw {
		displayName := a.Label
		if displayName == "" {
			displayName = a.PackageName
		}
		out[i] = driver.InstalledApp{BundleID: a.PackageName, DisplayName: displayName}
	}
	return out, nil
}

func (d *AndroidDriver) InstallApp(ctx context.Context, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "install", "-r", source); err != nil {
		return wrapAppLifecycleError(diag.CodeAppInstallFailed,
			fmt.Sprintf("could not install %s", source),
			"Confirm the app package is valid for this device, then try installing it again.",
			err)
	}
	return nil
}

func (d *AndroidDriver) UninstallApp(ctx context.Context, appID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "uninstall", appID); err != nil {
		return wrapAppLifecycleError(diag.CodeAppUninstallFailed,
			fmt.Sprintf("could not uninstall %s", appID),
			"Confirm the app is present on the device, then try again.",
			err)
	}
	return nil
}

// ClearAppData clears appID's data in place via the platform's native
// clear primitive. The app stays installed; opts.Source is unused
// because Android needs no artifact to reach a clean state.
func (d *AndroidDriver) ClearAppData(ctx context.Context, appID string, opts driver.ClearAppDataOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "shell", "pm", "clear", appID); err != nil {
		return wrapAppLifecycleError(diag.CodeAppDataClearFailed,
			fmt.Sprintf("could not clear data for %s", appID),
			"Confirm the app is installed on the device, then try clearing its data again.",
			err)
	}
	return nil
}

// wrapAppLifecycleError classifies a process-runner failure for an
// app-lifecycle method that shells out through adb. A failure the
// process runner already classified, such as its own fallback
// timeout, keeps that classification unchanged instead of being
// folded into code, so a caller sees diag.CodeToolTimeout rather than
// a generic install or uninstall failure when the underlying adb
// invocation hung. Every other failure is wrapped as code, matching
// what these methods have always returned.
func wrapAppLifecycleError(code, message, remedy string, err error) error {
	if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
		return err
	}
	return diag.New(code, message, remedy, err)
}

func (d *AndroidDriver) IsAppInstalled(ctx context.Context, appID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	result, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "shell", "pm", "list", "packages", appID)
	if err != nil {
		return false, err
	}
	return parsePackageListOutput(result.Stdout, appID), nil
}

// parsePackageListOutput requires an exact match on a "package:<id>"
// line, because "pm list packages <filter>" can return more than one
// line when filter is a prefix of several package ids.
func parsePackageListOutput(stdout, appID string) bool {
	want := "package:" + appID
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func (d *AndroidDriver) Screenshot(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := d.transport.Screenshot(ctx)
	if err != nil {
		return nil, d.fail(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw.Base64)
	if err != nil {
		return nil, d.fail(fmt.Errorf("androiddriver: decode screenshot: %w", err))
	}
	return decoded, nil
}

// DeviceInfo reports a minimal descriptor, because the agent exposes
// no device-info route. PlatformVersion and Model are always
// "unknown"; only UDID (the device serial) and Kind (derived from the
// serial's "emulator-" prefix) carry real information.
func (d *AndroidDriver) DeviceInfo(ctx context.Context) (driver.DeviceInfo, error) {
	if err := ctx.Err(); err != nil {
		return driver.DeviceInfo{}, err
	}
	kind := driver.DeviceKindDevice
	if strings.HasPrefix(d.opts.Serial, "emulator-") {
		kind = driver.DeviceKindEmulator
	}
	return driver.DeviceInfo{
		Platform:        "android",
		PlatformVersion: "unknown",
		Model:           "unknown",
		UDID:            d.opts.Serial,
		Kind:            kind,
	}, nil
}

// ScreenSize has no dedicated agent route. The /tree response's root
// is a synthetic wrapper with no bounds of its own, holding the real
// window roots (status bar excluded, main window, optional IME
// window) as its children, so this derives the screen size from the
// largest-area child instead.
func (d *AndroidDriver) ScreenSize(ctx context.Context) (wire.Size, error) {
	if err := ctx.Err(); err != nil {
		return wire.Size{}, err
	}
	tree, err := d.Hierarchy(ctx)
	if err != nil {
		return wire.Size{}, d.fail(err)
	}
	bounds, ok := findScreenBounds(tree)
	if !ok {
		return wire.Size{}, d.fail(errors.New("androiddriver: no child of the root has bounds, cannot derive screen dimensions"))
	}
	return wire.Size{Width: bounds.Right - bounds.Left, Height: bounds.Bottom - bounds.Top}, nil
}
