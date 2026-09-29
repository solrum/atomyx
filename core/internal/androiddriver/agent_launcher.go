package androiddriver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// Component identifiers the Android agent APK registers under.
// Rebinding accessibility across a reinstall requires the exact
// component form Android's `settings put` accepts, and starting the
// foreground service requires the exact form `am start-foreground-
// service -n` accepts; the two differ (one spells the package twice,
// the other uses the relative `.control` shorthand) because that is
// what the manifest declares for each component.
const (
	agentPackage         = "dev.atomyx.agent"
	accessibilityService = agentPackage + "/" + agentPackage + ".service.AtomyxAccessibilityService"
	foregroundService    = agentPackage + "/.control.AtomyxForegroundService"
)

const (
	defaultAgentPort = 8765

	// healthProbeTimeout bounds a single /health request. It is a real
	// wall-clock deadline on the outgoing HTTP call, not part of the
	// injected Clock, because it aborts one in-flight request rather
	// than governing a sleep or a retry budget.
	healthProbeTimeout = 1 * time.Second

	// rebindSettleMs is the pause Android needs after each accessibility
	// settings write to apply before the next command depends on it.
	rebindSettleMs int64 = 1_000

	// healthPollIntervalMs is the pause between /health attempts while
	// waiting for the agent to come up after the service is started.
	healthPollIntervalMs int64 = 500

	// defaultLaunchTimeoutMs bounds the total wait for /health to
	// report ready after the foreground service starts.
	defaultLaunchTimeoutMs int64 = 30_000

	apkPathEnvVar      = "ATOMYX_ANDROID_APK"
	apkAscentMaxLevels = 8
)

// apkRelPath is the debug APK's location relative to the repo root,
// used both as the default install artifact and as the marker
// resolveAPKPath ascends parent directories looking for.
var apkRelPath = filepath.Join("platforms", "android-agent", "app", "build", "outputs", "apk", "debug", "app-debug.apk")

// AgentLauncherErrorCode classifies why EnsureRunning failed, so a
// caller can distinguish a missing build artifact from a device-side
// failure without parsing the message.
type AgentLauncherErrorCode string

const (
	ErrCodeNoAdb         AgentLauncherErrorCode = "no-adb"
	ErrCodeNoAPK         AgentLauncherErrorCode = "no-apk"
	ErrCodeInstallFailed AgentLauncherErrorCode = "install-failed"
	ErrCodeRebindFailed  AgentLauncherErrorCode = "rebind-failed"
	ErrCodeLaunchTimeout AgentLauncherErrorCode = "launch-timeout"
)

// AgentLauncherError is the error EnsureRunning returns for a failure
// specific to bringing the agent up, as opposed to a raw adb or
// context failure it passes through unwrapped.
type AgentLauncherError struct {
	Code    AgentLauncherErrorCode
	Message string
}

func (e *AgentLauncherError) Error() string { return e.Message }

// HealthProber is the narrow slice of the agent's HTTP surface
// AgentLauncher depends on: the /health route it probes for
// readiness. Declaring it here, rather than depending on the wider
// Transport interface, lets a test substitute a fake that never opens
// a socket.
type HealthProber interface {
	Health(ctx context.Context) (transport.HealthResponse, error)
}

var _ HealthProber = (*transport.HTTPClient)(nil)

// AgentLauncherOptions configures an AgentLauncher.
type AgentLauncherOptions struct {
	// Serial is the Android device serial from "adb devices".
	Serial string

	// HostPort is the host-side TCP port forwarded to the device.
	// Defaults to 8765.
	HostPort int

	// DevicePort is the device-side port the agent's HTTP server
	// listens on. Defaults to 8765.
	DevicePort int

	// APKPath is an absolute path to the agent APK. When empty,
	// resolveAPKPath falls back to the ATOMYX_ANDROID_APK environment
	// variable, then to a repo-relative default location.
	APKPath string

	// LaunchTimeoutMs bounds how long EnsureRunning waits for /health
	// to report ready after starting the foreground service. Defaults
	// to 30 seconds when zero.
	LaunchTimeoutMs int64
}

// AgentLauncher brings the Android agent APK up on a connected device
// or emulator and confirms it is answering HTTP. EnsureRunning runs:
//
//  1. adb forward (idempotent) so the host can reach the device's
//     agent port.
//  2. A /health probe. If already ready, EnsureRunning returns
//     immediately.
//  3. Install of the agent APK.
//  4. A rebind of the accessibility service, because Android caches
//     the previous binding across an APK reinstall.
//  5. A start of the foreground service so the HTTP server listens.
//  6. A poll of /health until it reports ready or the launch timeout
//     elapses.
//
// EnsureRunning is idempotent: calling it repeatedly is cheap once
// step 2's probe passes.
type AgentLauncher struct {
	proc   process.Runner
	clock  clock.Clock
	prober HealthProber
	opts   AgentLauncherOptions

	hostPort   int
	devicePort int
}

// NewAgentLauncher constructs an AgentLauncher. proc issues every adb
// command, clock governs every sleep and timeout, and prober answers
// the /health readiness check. All three are injected so a test runs
// the full sequence without a real device, adb binary, or socket.
func NewAgentLauncher(proc process.Runner, clock clock.Clock, prober HealthProber, opts AgentLauncherOptions) *AgentLauncher {
	hostPort := opts.HostPort
	if hostPort == 0 {
		hostPort = defaultAgentPort
	}
	devicePort := opts.DevicePort
	if devicePort == 0 {
		devicePort = defaultAgentPort
	}
	return &AgentLauncher{
		proc:       proc,
		clock:      clock,
		prober:     prober,
		opts:       opts,
		hostPort:   hostPort,
		devicePort: devicePort,
	}
}

// EnsureRunning brings the agent up per the pipeline documented on
// AgentLauncher and returns once /health reports ready. It returns a
// non-nil *AgentLauncherError for a failure specific to this pipeline
// (missing APK, failed install, failed rebind, launch timeout), or
// the underlying error unwrapped for an adb or context failure.
func (l *AgentLauncher) EnsureRunning(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, err := l.proc.Run(ctx, "adb",
		"-s", l.opts.Serial, "forward",
		fmt.Sprintf("tcp:%d", l.hostPort),
		fmt.Sprintf("tcp:%d", l.devicePort),
	); err != nil {
		return fmt.Errorf("agentlauncher: adb forward: %w", err)
	}

	if l.probeHealth(ctx) {
		return nil
	}

	apk := l.resolveAPKPath()
	if _, err := os.Stat(apk); err != nil {
		return &AgentLauncherError{
			Code: ErrCodeNoAPK,
			Message: fmt.Sprintf(
				"APK not found at %s. Build it with `cd platforms/android-agent && ./gradlew :app:assembleDebug` or set %s env var.",
				apk, apkPathEnvVar,
			),
		}
	}

	if err := l.adbInstall(ctx, apk); err != nil {
		return err
	}

	if err := l.rebindAccessibility(ctx); err != nil {
		return err
	}

	if err := l.startForegroundService(ctx); err != nil {
		return fmt.Errorf("agentlauncher: start foreground service: %w", err)
	}

	return l.waitForHealth(ctx)
}

// resolveAPKPath resolves the agent APK's path in priority order: an
// explicit option first, then the ATOMYX_ANDROID_APK environment
// variable, then an ascent from the running binary's directory and
// the current working directory looking for the repo-relative
// default path, up to apkAscentMaxLevels parent directories per
// anchor. When nothing is found it returns the default path relative
// to the current working directory, so the caller's existence check
// reports an actionable location.
func (l *AgentLauncher) resolveAPKPath() string {
	if l.opts.APKPath != "" {
		return l.opts.APKPath
	}
	if fromEnv := os.Getenv(apkPathEnvVar); fromEnv != "" {
		return fromEnv
	}

	var anchors []string
	if exe, err := os.Executable(); err == nil {
		anchors = append(anchors, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		anchors = append(anchors, cwd)
	}

	for _, start := range anchors {
		dir := start
		for i := 0; i < apkAscentMaxLevels; i++ {
			candidate := filepath.Join(dir, apkRelPath)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	cwd, _ := os.Getwd()
	return filepath.Join(cwd, apkRelPath)
}

func (l *AgentLauncher) adbInstall(ctx context.Context, apk string) error {
	result, err := l.proc.Run(ctx, "adb", "-s", l.opts.Serial, "install", "-r", apk)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return &AgentLauncherError{
				Code:    ErrCodeNoAdb,
				Message: "adb not found on PATH. Install Android platform-tools.",
			}
		}
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = err.Error()
		}
		return &AgentLauncherError{
			Code:    ErrCodeInstallFailed,
			Message: fmt.Sprintf("adb install failed: %s", message),
		}
	}
	return nil
}

func (l *AgentLauncher) rebindAccessibility(ctx context.Context) error {
	if err := l.rebindAccessibilitySteps(ctx); err != nil {
		return &AgentLauncherError{
			Code:    ErrCodeRebindFailed,
			Message: fmt.Sprintf("accessibility rebind failed: %s", err.Error()),
		}
	}
	return nil
}

// rebindAccessibilitySteps runs the delete-then-put sequence Android
// requires to pick up a fresh binding: a stale enabled_accessibility_
// services value survives an APK reinstall, so it must be cleared
// before the new value is written, and each write needs a settle
// pause before the next command depends on it having applied.
func (l *AgentLauncher) rebindAccessibilitySteps(ctx context.Context) error {
	if _, err := l.adbShell(ctx, "settings", "delete", "secure", "enabled_accessibility_services"); err != nil {
		return err
	}
	if err := l.clock.Sleep(ctx, rebindSettleMs); err != nil {
		return err
	}
	if _, err := l.adbShell(ctx, "settings", "put", "secure", "enabled_accessibility_services", accessibilityService); err != nil {
		return err
	}
	return l.clock.Sleep(ctx, rebindSettleMs)
}

func (l *AgentLauncher) startForegroundService(ctx context.Context) error {
	_, err := l.adbShell(ctx, "am", "start-foreground-service", "-n", foregroundService)
	return err
}

func (l *AgentLauncher) adbShell(ctx context.Context, argv ...string) (process.Result, error) {
	args := append([]string{"-s", l.opts.Serial, "shell"}, argv...)
	return l.proc.Run(ctx, "adb", args...)
}

// waitForHealth polls /health at healthPollIntervalMs until it
// reports ready or the launch timeout elapses.
func (l *AgentLauncher) waitForHealth(ctx context.Context) error {
	timeoutMs := l.opts.LaunchTimeoutMs
	if timeoutMs == 0 {
		timeoutMs = defaultLaunchTimeoutMs
	}

	deadline := l.clock.NowMs() + timeoutMs
	for l.clock.NowMs() < deadline {
		if l.probeHealth(ctx) {
			return nil
		}
		if err := l.clock.Sleep(ctx, healthPollIntervalMs); err != nil {
			return err
		}
	}

	return &AgentLauncherError{
		Code: ErrCodeLaunchTimeout,
		Message: fmt.Sprintf(
			"Android agent did not answer /health on port %d within %ds.",
			l.hostPort, timeoutMs/1000,
		),
	}
}

// probeHealth reports true only when /health responds with
// {ok:true, accessibilityConnected:true}. A missing connection
// (accessibility disabled) counts as not ready because every agent
// route needs the service bound, and a transport failure counts as
// not ready rather than as an error, so a caller can poll it in a
// loop without special-casing "not up yet".
func (l *AgentLauncher) probeHealth(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	resp, err := l.prober.Health(probeCtx)
	if err != nil {
		return false
	}
	return resp.Ok && resp.AccessibilityConnected
}
