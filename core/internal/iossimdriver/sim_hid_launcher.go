package iossimdriver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/process"
)

// simHidBundleRelPath is the atomyx-sim-hid helper's location inside
// its packaged app bundle, relative to whichever candidate directory
// resolveSimHidBinary checks.
const simHidBundleRelPath = "atomyx-sim-hid.app/Contents/MacOS/atomyx-sim-hid"

// simHidHandshakeTransport is the only transport value
// waitForSimHidHandshake accepts. The helper reports it on the first
// line it writes to stdout once its WebSocket listener is up.
const simHidHandshakeTransport = "ws-input"

// simHidBinEnv overrides the resolved helper binary path, set by
// tests and by a packaged build that places the helper somewhere
// other than the well-known candidate paths. It is distinct from the
// ATOMYX_SIM_HID opt-in toggle device.DriverFactory reads: the two
// must not be conflated, or a caller reading one for the other would
// try to spawn the literal string "1" as a binary path.
const simHidBinEnv = "ATOMYX_SIM_HID_BIN"

// SimHidLauncherErrorCode names one of the known ways starting the
// atomyx-sim-hid helper can fail, so a caller can branch on Code
// instead of matching Message.
type SimHidLauncherErrorCode string

const (
	SimHidErrBinaryNotFound      SimHidLauncherErrorCode = "binary-not-found"
	SimHidErrExitedEarly         SimHidLauncherErrorCode = "exited-early"
	SimHidErrInvalidHandshake    SimHidLauncherErrorCode = "invalid-handshake"
	SimHidErrUnexpectedHandshake SimHidLauncherErrorCode = "unexpected-handshake"
)

// SimHidLauncherError is a named, actionable failure from
// SimHidLauncher.Launch, so a caller can branch on Code instead of
// matching Message.
type SimHidLauncherError struct {
	Code    SimHidLauncherErrorCode
	Message string
}

func (e *SimHidLauncherError) Error() string { return e.Message }

// SimHidLaunchResult is what a successful Launch returns: the
// ws-input port the helper's handshake reported, and Kill to
// terminate the still-running helper process later.
type SimHidLaunchResult struct {
	Port int
	Kill func() error
}

// SimHidLauncher spawns the atomyx-sim-hid helper for one simulator
// UDID and waits for its readiness handshake. It owns no per-UDID
// state of its own. Dedup and state tracking belong to the caller
// (device.SimHidManager).
type SimHidLauncher struct {
	bg process.BackgroundRunner
}

// NewSimHidLauncher constructs a SimHidLauncher. bg is injected so a
// test can substitute a process.FakeBackground and script the
// helper's handshake without a real atomyx-sim-hid binary.
func NewSimHidLauncher(bg process.BackgroundRunner) *SimHidLauncher {
	return &SimHidLauncher{bg: bg}
}

// Launch resolves the helper binary, spawns it with ["--udid", udid],
// and blocks until its first stdout line is a valid ws-input
// handshake, the process exits first, or ctx is canceled first. A
// successful Launch leaves the helper process running, and the caller
// uses the returned Kill to stop it later. A failed Launch has
// already killed whatever it spawned, so the caller has nothing left
// to clean up.
func (l *SimHidLauncher) Launch(ctx context.Context, udid string) (SimHidLaunchResult, error) {
	binPath, err := resolveSimHidBinary()
	if err != nil {
		return SimHidLaunchResult{}, err
	}

	proc, err := l.bg.Start(ctx, binPath, "--udid", udid)
	if err != nil {
		return SimHidLaunchResult{}, err
	}

	port, err := waitForSimHidHandshake(ctx, proc)
	if err != nil {
		_ = proc.Kill()
		return SimHidLaunchResult{}, err
	}
	return SimHidLaunchResult{Port: port, Kill: proc.Kill}, nil
}

// simHidHandshake is the JSON line the helper writes to stdout once
// its WebSocket listener is up, matching platforms/ios-agent's Swift
// helper's emitted shape field for field. Port is a pointer so a
// present-but-zero port (a valid, if unusual, handshake) is
// distinguished from a missing one.
type simHidHandshake struct {
	Event     string `json:"event"`
	Port      *int   `json:"port"`
	Transport string `json:"transport"`
}

// waitForSimHidHandshake reads proc's first newline-terminated stdout
// line and validates it as a ws-input handshake. The process exiting,
// or closing stdout, before a full line arrives surfaces as
// SimHidErrExitedEarly. A full line that fails to parse as JSON
// surfaces as SimHidErrInvalidHandshake, and one that parses but
// fails the transport or port check surfaces as
// SimHidErrUnexpectedHandshake. ctx has no effect on an in-flight
// stdout read, since bufio.Reader.ReadString itself is not
// ctx-aware; it is threaded through only so a caller that has already
// been canceled is not left resolving a race that has no arm to check
// it against.
func waitForSimHidHandshake(ctx context.Context, proc process.BackgroundProcess) (int, error) {
	reader := bufio.NewReader(proc.Stdout())
	var port int
	probe := func() (bool, error) {
		line, err := reader.ReadString('\n')
		if err != nil {
			result, waitErr := proc.Wait()
			detail := waitErr
			if detail == nil {
				detail = err
			}
			return false, &SimHidLauncherError{
				Code: SimHidErrExitedEarly,
				Message: fmt.Sprintf(
					"atomyx-sim-hid helper exited before completing the handshake (exit code %d): %v",
					result.ExitCode, detail,
				),
			}
		}

		trimmed := strings.TrimSpace(line)
		var hs simHidHandshake
		if jsonErr := json.Unmarshal([]byte(trimmed), &hs); jsonErr != nil {
			return false, &SimHidLauncherError{
				Code:    SimHidErrInvalidHandshake,
				Message: fmt.Sprintf("invalid handshake JSON: %s", trimmed),
			}
		}
		if hs.Transport != simHidHandshakeTransport || hs.Port == nil {
			return false, &SimHidLauncherError{
				Code:    SimHidErrUnexpectedHandshake,
				Message: fmt.Sprintf("unexpected handshake: %s", trimmed),
			}
		}
		port = *hs.Port
		return true, nil
	}

	// The handshake line is the only readiness signal: a failed read
	// already means the process exited, so there is no separate exit
	// channel to race, and no poll interval or timeout, since probe
	// blocks until stdout has a line or the read fails.
	if _, err := process.AwaitReady(ctx, clock.SystemClock{}, probe, nil, 0, 0); err != nil {
		return 0, err
	}
	return port, nil
}

// resolveSimHidBinary resolves the path to the atomyx-sim-hid helper
// binary: ATOMYX_SIM_HID_BIN when set (an explicit override for tests
// and for a packaged build that places the helper somewhere other
// than the candidates below), else the first executable candidate
// among the well-known locations matching the Tauri bundle layout.
func resolveSimHidBinary() (string, error) {
	if envPath := os.Getenv(simHidBinEnv); envPath != "" {
		return envPath, nil
	}

	candidates := simHidBinaryCandidates()
	for _, p := range candidates {
		if isExecutableFile(p) {
			return p, nil
		}
	}

	return "", &SimHidLauncherError{
		Code: SimHidErrBinaryNotFound,
		Message: "atomyx-sim-hid helper not found. " +
			"Build it with `bash apps/studio/src-tauri/helpers/atomyx-sim-hid/build.sh` " +
			"or set ATOMYX_SIM_HID_BIN to the bundle executable path.",
	}
}

// simHidBinaryCandidates lists the paths resolveSimHidBinary checks,
// in priority order: next to the current executable (the production
// Tauri build layout), then a handful of paths relative to the
// current working directory that match where the helper lands in a
// monorepo dev checkout.
func simHidBinaryCandidates() []string {
	var candidates []string
	if execPath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(execPath), simHidBundleRelPath))
	}
	candidates = append(candidates,
		filepath.Join("apps/studio/src-tauri/helpers/atomyx-sim-hid", simHidBundleRelPath),
		filepath.Join("src-tauri/helpers/atomyx-sim-hid", simHidBundleRelPath),
		filepath.Join("helpers/atomyx-sim-hid", simHidBundleRelPath),
	)
	return candidates
}

// isExecutableFile reports whether path names a regular file with at
// least one executable bit set.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
