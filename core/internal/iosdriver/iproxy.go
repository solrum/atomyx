package iosdriver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"sync/atomic"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/process"
)

// Tuning knobs for the iproxy USB tunnel lifecycle. See launchIproxy
// for the guards these back.
const (
	// iproxyPortProbeTimeout bounds a single one-shot check of whether
	// 127.0.0.1:port currently has a listener.
	iproxyPortProbeTimeout = 300 * time.Millisecond
	// iproxyPingProbeTimeout bounds the ping handshake used to tell an
	// existing Atomyx driver tunnel apart from an unrelated listener.
	iproxyPingProbeTimeout = 1 * time.Second
	// iproxyTunnelPollInterval is the spacing between readiness checks
	// while waiting for a freshly spawned iproxy to come up.
	iproxyTunnelPollInterval = 200 * time.Millisecond
	// iproxyTunnelUpTimeout is the total budget for a freshly spawned
	// iproxy tunnel to become reachable.
	iproxyTunnelUpTimeout = 5 * time.Second
	// iproxyBusyProbeAttempts is how many ping probes a listener that accepts
	// but stays silent gets before the runner is reported busy. The runner
	// serves one client at a time and notices a closed client on its next
	// read, which lands within about a second, so three probes spaced by
	// iproxyBusyRetryPause ride out a client that just closed while a
	// client that stays connected is reported within about four seconds.
	iproxyBusyProbeAttempts = 3
	// iproxyBusyRetryPause is the wait between two probes of a silent
	// listener.
	iproxyBusyRetryPause = 500 * time.Millisecond
)

// deviceRunnerPort is the port the runner listens on inside a physical
// device. iproxy maps each per-device host port to it, so concurrent
// devices never share a host port.
const deviceRunnerPort = defaultIosPort

// unmanagedRunnerWarning is the warning raised when a simulator
// runner is adopted without a launch identifier.
func unmanagedRunnerWarning(port int) string {
	return fmt.Sprintf("adopting the runner on 127.0.0.1:%d without a launch id because it was not started by this process", port)
}

// IproxyErrorCode names one of the known ways launching or waiting on
// the iproxy USB tunnel can fail.
type IproxyErrorCode string

const (
	// IproxyErrNotInstalled means the iproxy binary is not on PATH.
	IproxyErrNotInstalled IproxyErrorCode = "not-installed"
	// IproxyErrBindConflict means 127.0.0.1:port is already held by a
	// listener that does not answer the Atomyx ping handshake.
	IproxyErrBindConflict IproxyErrorCode = "bind-conflict"
	// IproxyErrRunnerBusy means 127.0.0.1:port accepts connections but the
	// listener behind it did not answer the ping handshake within the retry
	// budget, which usually means another client holds the runner.
	IproxyErrRunnerBusy IproxyErrorCode = "runner-busy"
	// IproxyErrTunnelTimeout means a freshly spawned iproxy never made
	// the port reachable within its budget.
	IproxyErrTunnelTimeout IproxyErrorCode = "tunnel-timeout"
	// IproxyErrSpawnExit means the iproxy process exited, for any
	// reason other than a missing binary, before the tunnel came up.
	IproxyErrSpawnExit IproxyErrorCode = "spawn-exit"
	// IproxyErrTunnelExit means the iproxy process exited on its own after
	// the tunnel had come up, so the device link was lost mid-session.
	IproxyErrTunnelExit IproxyErrorCode = "tunnel-exit"
)

// IproxyError is a named, actionable failure from launching or
// waiting for the iproxy USB tunnel used to reach a physical iOS
// device, so a caller can branch on Code instead of matching Message.
type IproxyError struct {
	Code    IproxyErrorCode
	Message string
}

func (e *IproxyError) Error() string { return e.Message }

// TunnelConfig configures StartTunnel.
type TunnelConfig struct {
	// UDID is the physical device the tunnel reaches.
	UDID string
	// HostPort is the host port the tunnel listens on.
	HostPort int
	// RunnerID is the launch identifier of the runner expected behind
	// the tunnel. Empty means identity rests on the UDID alone.
	RunnerID string
	// Warn receives a one-line warning when an existing runner is
	// adopted without a launch identifier. Nil discards it.
	Warn func(message string)
	// Reuse is asked once when a listener on HostPort already answers as
	// the expected runner. It returns the pid of the tunnel behind that
	// listener and true when the caller can account for that tunnel, which
	// means it has the tunnel's identity on record and can stop it. A nil
	// Reuse, or a false answer, refuses the listener with
	// IproxyErrBindConflict, because a tunnel nobody can stop would outlive
	// its user.
	Reuse func() (pid int, ok bool)
	// FreshPort marks HostPort as reserved for a runner the caller has just
	// spawned, so no other client of the runner can legitimately hold it.
	// A listener there that accepts but stays silent then lost the race
	// for the port and is refused with IproxyErrBindConflict, which lets a
	// caller retry on a new port. Without it, a silent listener is
	// refused with IproxyErrRunnerBusy.
	FreshPort bool
	// OnExit receives the verdict when the tunnel process exits on its own
	// after it became reachable. A stop through the returned stop function
	// is not reported. Nil watches nothing.
	OnExit func(verdict error)
}

// StartTunnel starts the USB tunnel for a physical device by running
// "iproxy <hostPort>:22087 -u <udid>", and does not return until the
// tunnel is confirmed reachable or one of the IproxyErrorCode failure
// modes is confirmed instead. iproxy is a long-running tunnel process,
// not a command that completes, so it is started through bg.
// Runner.Run applies a fallback timeout to a deadline-less context,
// which would kill the tunnel mid-session. A successful start keeps the
// process running, independent of ctx, until the returned stop function
// (or this function's own cleanup on failure) kills its process group.
// stop is nil and pid is what Reuse reported when an existing tunnel to the
// expected runner was reused, because the caller stops that tunnel
// through the identity it holds on record. Otherwise pid is the iproxy
// process, which leads its own process group, so a later process can find
// and end it.
//
// Two guards make it safe to dial immediately after this returns
// successfully:
//
//   - Port-conflict detection before spawn. If 127.0.0.1:hostPort
//     already has a listener, it is probed with the same ping handshake
//     this driver's own wire protocol speaks. A listener that answers as
//     the expected runner (see ExpectedRunner) is an existing Atomyx
//     driver tunnel and is reused without spawning a second iproxy, but
//     only when cfg.Reuse accounts for it. A listener that does not
//     answer in a non-runner way, answers as a different runner, or is not
//     accounted for is refused with IproxyErrBindConflict instead of
//     racing whatever else is bound there. A listener that accepts but
//     stays silent is probed up to iproxyBusyProbeAttempts times, then
//     refused with IproxyErrRunnerBusy, or with IproxyErrBindConflict when
//     cfg.FreshPort says the port was just reserved for a new runner.
//     A cancelled ctx during the probe returns ctx.Err() instead.
//   - Poll for real tunnel connectivity after spawn. iproxy emits no
//     "ready" signal on stdout or stderr, so this polls the port on
//     iproxyTunnelPollInterval until a fresh TCP connect succeeds,
//     racing that poll against the process exiting early (a bad
//     UDID, iproxy missing from PATH, a device-trust failure, or any
//     other spawn failure) so a caller never dials a tunnel that has
//     already failed. The poll cadence and the iproxyTunnelUpTimeout
//     deadline both run against clk, so a test can drive
//     IproxyErrTunnelTimeout deterministically with a fake clock
//     instead of waiting on real time.
//
// The tunnel is up when the host port accepts connections, which can
// precede the runner answering on the device, so a caller that starts
// the tunnel before the runner polls the ping handshake afterwards.
func StartTunnel(ctx context.Context, bg process.BackgroundRunner, clk clock.Clock, cfg TunnelConfig) (stop func(), pid int, err error) {
	if canConnectTCP(ctx, cfg.HostPort, iproxyPortProbeTimeout) {
		want := ExpectedRunner{UDID: cfg.UDID, Kind: KindDevice, RunnerID: cfg.RunnerID}
		got, outcome := probeListener(ctx, clk, cfg.HostPort)
		if outcome == probeCancelled {
			return nil, 0, ctx.Err()
		}
		if outcome == probeSilent {
			code := IproxyErrRunnerBusy
			if cfg.FreshPort {
				code = IproxyErrBindConflict
			}
			return nil, 0, &IproxyError{
				Code: code,
				Message: fmt.Sprintf(
					"the listener on 127.0.0.1:%d did not answer, it may be busy with another client or not be an Atomyx runner",
					cfg.HostPort,
				),
			}
		}
		found, unmanaged := false, false
		if outcome == probeAnswered {
			var idErr *RunnerIdentityError
			found, unmanaged, idErr = judgeRunner(cfg.HostPort, got, want)
			if idErr != nil {
				return nil, 0, &IproxyError{Code: IproxyErrBindConflict, Message: idErr.Error()}
			}
		}
		if found {
			if unmanaged && cfg.Warn != nil {
				cfg.Warn(unmanagedRunnerWarning(cfg.HostPort))
			}
			reusedPid, accounted := 0, false
			if cfg.Reuse != nil {
				reusedPid, accounted = cfg.Reuse()
			}
			if accounted {
				return nil, reusedPid, nil
			}
			return nil, 0, &IproxyError{
				Code: IproxyErrBindConflict,
				Message: fmt.Sprintf(
					"port %d on 127.0.0.1 already has a tunnel to device %s that this process cannot account for and so cannot stop",
					cfg.HostPort, cfg.UDID,
				),
			}
		}
		return nil, 0, &IproxyError{
			Code: IproxyErrBindConflict,
			Message: fmt.Sprintf(
				"port %d on 127.0.0.1 already has a listener that is not an Atomyx driver tunnel; "+
					"stop whatever is bound there before connecting to device %s",
				cfg.HostPort, cfg.UDID,
			),
		}
	}

	portMapping := fmt.Sprintf("%d:%d", cfg.HostPort, deviceRunnerPort)
	proc, startErr := bg.Start(context.WithoutCancel(ctx), "iproxy", portMapping, "-u", cfg.UDID)
	if startErr != nil {
		return nil, 0, classifyIproxyExit(startErr)
	}
	var tunnelReady atomic.Bool
	exited, stopProc := superviseBackground(proc, func(_ process.BackgroundResult, err error) error {
		if tunnelReady.Load() {
			return classifyIproxyTunnelExit(err)
		}
		return classifyIproxyExit(err)
	})

	probe := func() (bool, error) {
		return canConnectTCP(ctx, cfg.HostPort, iproxyPortProbeTimeout), nil
	}
	outcome, err := process.AwaitReady(
		ctx, clk, probe, exited,
		iproxyTunnelPollInterval.Milliseconds(), iproxyTunnelUpTimeout.Milliseconds(),
	)
	switch outcome {
	case process.ReadinessReady:
		tunnelReady.Store(true)
		var stopping atomic.Bool
		notifyOnExit(exited, &stopping, cfg.OnExit)
		return func() {
			stopping.Store(true)
			stopProc()
		}, proc.Pid(), nil
	case process.ReadinessExited:
		stopProc()
		return nil, 0, err
	case process.ReadinessTimeout:
		stopProc()
		return nil, 0, &IproxyError{
			Code: IproxyErrTunnelTimeout,
			Message: fmt.Sprintf(
				"iproxy tunnel did not become reachable on 127.0.0.1:%d within %s; "+
					"is the Swift driver actually running on device %s?",
				cfg.HostPort, iproxyTunnelUpTimeout, cfg.UDID,
			),
		}
	default: // process.ReadinessCtxDone
		stopProc()
		return nil, 0, err
	}
}

// launchIproxy starts the tunnel for this driver's device and keeps its
// stop function for Disconnect. Calling it while a tunnel is already
// running replaces it, matching Disconnect followed by a fresh Connect.
func (d *IosDriver) launchIproxy(ctx context.Context) error {
	d.stopIproxy()
	stop, _, err := StartTunnel(ctx, d.background, d.opts.Clock, TunnelConfig{
		UDID: d.opts.UDID, HostPort: d.opts.Port, RunnerID: d.opts.RunnerID, Warn: d.opts.Warn,
		// The driver keeps no record of tunnels, so it keeps using one that
		// already reaches its runner and leaves it running.
		Reuse: func() (int, bool) { return 0, true },
	})
	if err != nil {
		return err
	}
	d.iproxyStop = stop
	return nil
}

// classifyIproxyExit turns the error from starting or waiting on a
// failed iproxy launch into a named IproxyError. A missing binary is
// detected via exec.ErrNotFound, the sentinel os/exec returns when
// PATH lookup fails at start. Every other exit is reported
// generically, since the BackgroundRunner carries no stderr
// classification of its own beyond that sentinel.
func classifyIproxyExit(err error) *IproxyError {
	if err != nil && errors.Is(err, exec.ErrNotFound) {
		return &IproxyError{
			Code:    IproxyErrNotInstalled,
			Message: "iproxy not found on PATH; install libimobiledevice (brew install libimobiledevice) and retry",
		}
	}
	return &IproxyError{
		Code:    IproxyErrSpawnExit,
		Message: fmt.Sprintf("iproxy exited before the tunnel became reachable: %v", err),
	}
}

// classifyIproxyTunnelExit names the exit of an iproxy process that had
// already made the tunnel reachable.
func classifyIproxyTunnelExit(err error) *IproxyError {
	message := "iproxy exited after the tunnel was reachable"
	if err != nil {
		message = fmt.Sprintf("%s: %v", message, err)
	}
	return &IproxyError{Code: IproxyErrTunnelExit, Message: message}
}

// canConnectTCP reports whether 127.0.0.1:port currently accepts a
// TCP connection.
func canConnectTCP(ctx context.Context, port int, timeout time.Duration) bool {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := new(net.Dialer).DialContext(dialCtx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// stopIproxy kills a running iproxy tunnel's process group and waits
// for it to exit, then forgets it. It is a no-op when no tunnel was
// launched.
func (d *IosDriver) stopIproxy() {
	if d.iproxyStop == nil {
		return
	}
	d.iproxyStop()
	d.iproxyStop = nil
}

// tunnelPingTimeout bounds one identity probe made by StartTunnel.
var tunnelPingTimeout = iproxyPingProbeTimeout

// probeListener pings the listener on port, retrying while it accepts
// but stays silent, up to iproxyBusyProbeAttempts probes. Any other
// outcome ends the loop at once.
func probeListener(ctx context.Context, clk clock.Clock, port int) (RunnerIdentity, probeOutcome) {
	for attempt := 1; ; attempt++ {
		got, outcome := probeRunner(ctx, port, tunnelPingTimeout)
		if outcome != probeSilent || attempt >= iproxyBusyProbeAttempts {
			return got, outcome
		}
		if err := clk.Sleep(ctx, iproxyBusyRetryPause.Milliseconds()); err != nil {
			return got, probeCancelled
		}
	}
}
