package iosdriver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"time"

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
)

// IproxyErrorCode names one of the known ways launching or waiting on
// the iproxy USB tunnel can fail.
type IproxyErrorCode string

const (
	// IproxyErrNotInstalled means the iproxy binary is not on PATH.
	IproxyErrNotInstalled IproxyErrorCode = "not-installed"
	// IproxyErrBindConflict means 127.0.0.1:port is already held by a
	// listener that does not answer the Atomyx ping handshake.
	IproxyErrBindConflict IproxyErrorCode = "bind-conflict"
	// IproxyErrTunnelTimeout means a freshly spawned iproxy never made
	// the port reachable within its budget.
	IproxyErrTunnelTimeout IproxyErrorCode = "tunnel-timeout"
	// IproxyErrSpawnExit means the iproxy process exited, for any
	// reason other than a missing binary, before the tunnel came up.
	IproxyErrSpawnExit IproxyErrorCode = "spawn-exit"
)

// IproxyError is a named, actionable failure from launching or
// waiting for the iproxy USB tunnel used to reach a physical iOS
// device, so a caller can branch on Code instead of matching Message.
type IproxyError struct {
	Code    IproxyErrorCode
	Message string
}

func (e *IproxyError) Error() string { return e.Message }

// launchIproxy starts the USB tunnel for a physical device by running
// "iproxy <port>:<port> -u <udid>", and
// does not return until the tunnel is confirmed reachable or one of
// the IproxyErrorCode failure modes is confirmed instead. iproxy is a
// long-running tunnel process, not a command that completes, so it is
// started through the injected BackgroundRunner. Runner.Run applies a
// fallback timeout to a deadline-less context, which would kill the
// tunnel mid-session. A successful launch keeps the process running,
// independent of ctx, until stopIproxy (or this function's own
// cleanup on failure) kills its process group. Calling launchIproxy while a tunnel is already running replaces it,
// matching Disconnect followed by a fresh Connect.
//
// Two guards make it safe for Connect to dial immediately after this
// returns successfully:
//
//   - Port-conflict detection before spawn. If 127.0.0.1:port already
//     has a listener, it is probed with the same ping handshake this
//     driver's own wire protocol speaks. A listener that answers is
//     an existing Atomyx driver tunnel and is reused without spawning
//     a second iproxy; a listener that does not answer is refused
//     with IproxyErrBindConflict instead of racing whatever else is
//     bound there.
//   - Poll for real tunnel connectivity after spawn. iproxy emits no
//     "ready" signal on stdout or stderr, so this polls the port on
//     iproxyTunnelPollInterval until a fresh TCP connect succeeds,
//     racing that poll against the process exiting early (a bad
//     UDID, iproxy missing from PATH, a device-trust failure, or any
//     other spawn failure) so a caller never dials a tunnel that has
//     already failed. The poll cadence and the iproxyTunnelUpTimeout
//     deadline both run against d.opts.Clock, so a test can drive
//     IproxyErrTunnelTimeout deterministically with a fake clock
//     instead of waiting on real time.
func (d *IosDriver) launchIproxy(ctx context.Context) error {
	if canConnectTCP(ctx, d.opts.Port, iproxyPortProbeTimeout) {
		if probeIproxyPing(ctx, d.opts.Port, iproxyPingProbeTimeout) {
			return nil
		}
		return &IproxyError{
			Code: IproxyErrBindConflict,
			Message: fmt.Sprintf(
				"port %d on 127.0.0.1 already has a listener that is not an Atomyx driver tunnel; "+
					"stop whatever is bound there before connecting to device %s",
				d.opts.Port, d.opts.UDID,
			),
		}
	}

	portMapping := fmt.Sprintf("%d:%d", d.opts.Port, d.opts.Port)
	proc, startErr := d.background.Start(context.WithoutCancel(ctx), "iproxy", portMapping, "-u", d.opts.UDID)
	if startErr != nil {
		return classifyIproxyExit(startErr)
	}
	exited, stop := superviseBackground(proc, func(_ process.BackgroundResult, err error) error {
		return classifyIproxyExit(err)
	})

	probe := func() (bool, error) {
		return canConnectTCP(ctx, d.opts.Port, iproxyPortProbeTimeout), nil
	}
	outcome, err := process.AwaitReady(
		ctx, d.opts.Clock, probe, exited,
		iproxyTunnelPollInterval.Milliseconds(), iproxyTunnelUpTimeout.Milliseconds(),
	)
	switch outcome {
	case process.ReadinessReady:
		d.iproxyStop = stop
		return nil
	case process.ReadinessExited:
		stop()
		return err
	case process.ReadinessTimeout:
		stop()
		return &IproxyError{
			Code: IproxyErrTunnelTimeout,
			Message: fmt.Sprintf(
				"iproxy tunnel did not become reachable on 127.0.0.1:%d within %s; "+
					"is the Swift driver actually running on device %s?",
				d.opts.Port, iproxyTunnelUpTimeout, d.opts.UDID,
			),
		}
	default: // process.ReadinessCtxDone
		stop()
		return err
	}
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

// probeIproxyPing sends the same ping handshake Connect uses once the
// TCP transport is up, and reports whether the listener at
// 127.0.0.1:port answers it. This is how a pre-existing listener on
// the port is confirmed to be an Atomyx driver tunnel rather than
// some unrelated process.
func probeIproxyPing(ctx context.Context, port int, timeout time.Duration) bool {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := new(net.Dialer).DialContext(dialCtx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(`{"id":0,"type":"ping","args":{}}` + "\n")); err != nil {
		return false
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return false
	}
	var frame struct {
		Ok   *bool `json:"ok"`
		Data struct {
			Pong bool `json:"pong"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		return false
	}
	return frame.Ok != nil && *frame.Ok && frame.Data.Pong
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
