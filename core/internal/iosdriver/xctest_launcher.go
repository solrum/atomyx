package iosdriver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/process"
)

// Xcode project layout and command-surface defaults for the XCUITest
// driver build. These name the exact project file, scheme, build
// product, and test target the Swift driver ships under
// platforms/ios-agent. LaunchConfig's Scheme, ProjectFileName,
// RunnerAppName, and OnlyTestingTarget fields override the
// corresponding default on a per-instance basis; a caller renaming the
// underlying Xcode project artifact sets the matching field instead of
// editing the constant below.
const (
	xctestProjectFileName   = "AtomyxDriver.xcodeproj"
	xctestDefaultScheme     = "AtomyxDriver"
	xctestRunnerAppName     = "AtomyxDriverAgent-Runner.app"
	xctestOnlyTestingTarget = "AtomyxDriverAgent/AtomyxDriverAgent/testServeCommands"
	xctestConfigSimulator   = "Debug-iphonesimulator"
	xctestConfigDevice      = "Debug-iphoneos"
)

// Timing defaults and the poll cadence EnsureRunning uses while
// waiting for a freshly spawned driver to answer the ping handshake.
const (
	xctestDefaultBuildTimeoutMs  int64 = 120_000
	xctestDefaultLaunchTimeoutMs int64 = 90_000
	xctestPollIntervalMs         int64 = 500
)

// Byte budgets for the stderr excerpt included in a build or spawn
// failure message, so a caller gets enough context to diagnose
// without the message growing unbounded on a verbose xcodebuild
// failure.
const (
	xctestSigningStderrTailChars     = 500
	xctestBuildFailedStderrTailChars = 800
	xctestSpawnExitStderrTailChars   = 500
)

// xctestSourceDirNames are the project subdirectories EnsureRunning
// scans for a Swift file newer than the compiled test runner, to
// decide whether a rebuild is needed.
var xctestSourceDirNames = [...]string{"Tests", "Sources", "App"}

// XctestLauncherErrorCode names one of the known ways bringing up the
// XCUITest driver can fail, so a caller can branch on Code instead of
// matching Message.
type XctestLauncherErrorCode string

const (
	XctestErrNoXcode       XctestLauncherErrorCode = "no-xcode"
	XctestErrNoProject     XctestLauncherErrorCode = "no-project"
	XctestErrBuildFailed   XctestLauncherErrorCode = "build-failed"
	XctestErrSigning       XctestLauncherErrorCode = "signing"
	XctestErrLaunchTimeout XctestLauncherErrorCode = "launch-timeout"
	XctestErrSpawnExit     XctestLauncherErrorCode = "spawn-exit"
	XctestErrSimNotBooted  XctestLauncherErrorCode = "sim-not-booted"
)

// XctestLauncherError is a named, actionable failure from
// XctestLauncher.EnsureRunning, so a caller can branch on Code
// instead of matching Message.
type XctestLauncherError struct {
	Code    XctestLauncherErrorCode
	Message string
}

func (e *XctestLauncherError) Error() string { return e.Message }

// LaunchConfig configures an XctestLauncher.
type LaunchConfig struct {
	// ProjectDir is the directory containing AtomyxDriver.xcodeproj
	// (platforms/ios-agent in the Atomyx repo layout).
	ProjectDir string
	// Scheme is the xcodebuild scheme to build and test. Defaults to
	// the standard Atomyx driver scheme when empty.
	Scheme string
	// ProjectFileName is the .xcodeproj directory name inside
	// ProjectDir. Defaults to the standard Atomyx driver project file
	// name when empty.
	ProjectFileName string
	// Port is the host-side TCP port the Swift driver listens on.
	// Defaults to 22087 when zero.
	Port int
	// Kind selects the simulator or physical-device destination and
	// signing overrides.
	Kind Kind
	// UDID is the simulator or device UDID substituted into the
	// -destination argument.
	UDID string
	// DevTeam is the Apple Development Team ID passed as
	// DEVELOPMENT_TEAM. Required when Kind is KindDevice; the caller
	// resolves any ATOMYX_DEV_TEAM environment fallback before
	// constructing this config.
	DevTeam string
	// RunnerAppName is the compiled XCUITest runner product's file
	// name under the build output directory. Defaults to the standard
	// Atomyx driver runner app name when empty.
	RunnerAppName string
	// OnlyTestingTarget is the -only-testing selector xcodebuild uses
	// to run just the XCUITest driver's command-serving test. Defaults
	// to the standard Atomyx driver testing target when empty.
	OnlyTestingTarget string
	// BuildTimeoutMs bounds a single "xcodebuild build-for-testing"
	// invocation. Defaults to 120000 (120s) when zero.
	BuildTimeoutMs int64
	// LaunchTimeoutMs bounds the wait for the spawned driver to start
	// answering the ping handshake. Defaults to 90000 (90s) when zero.
	LaunchTimeoutMs int64
}

// scheme returns c.Scheme, falling back to the standard Atomyx driver
// scheme when empty.
func (c LaunchConfig) scheme() string {
	if c.Scheme != "" {
		return c.Scheme
	}
	return xctestDefaultScheme
}

// projectFileName returns c.ProjectFileName, falling back to the
// standard Atomyx driver .xcodeproj file name when empty.
func (c LaunchConfig) projectFileName() string {
	if c.ProjectFileName != "" {
		return c.ProjectFileName
	}
	return xctestProjectFileName
}

// runnerAppName returns c.RunnerAppName, falling back to the standard
// Atomyx driver runner app name when empty.
func (c LaunchConfig) runnerAppName() string {
	if c.RunnerAppName != "" {
		return c.RunnerAppName
	}
	return xctestRunnerAppName
}

// onlyTestingTarget returns c.OnlyTestingTarget, falling back to the
// standard Atomyx driver testing target when empty.
func (c LaunchConfig) onlyTestingTarget() string {
	if c.OnlyTestingTarget != "" {
		return c.OnlyTestingTarget
	}
	return xctestOnlyTestingTarget
}

// XctestLauncher brings up the Swift XCUITest driver: probe for an
// already-running instance, build it if the compiled test runner is
// missing or stale, spawn "xcodebuild test-without-building", and
// wait for it to answer the ping handshake on LaunchConfig.Port.
//
// XctestLauncher owns none of the spawned process's lifecycle beyond
// bringing it up and handing back a way to stop it: it does not track
// a running/not-running state or emit events. A consumer that needs
// to supervise the driver process (restart on crash, report status)
// composes that on top of EnsureRunning and the kill function it
// returns.
type XctestLauncher struct {
	proc  process.Runner
	clock clock.Clock
	cfg   LaunchConfig
}

// NewXctestLauncher constructs an XctestLauncher. proc and clock are
// injected so a test can substitute a process.Fake and a fake Clock
// for both the xcodebuild invocations and the poll timing, without a
// real Xcode toolchain or device.
func NewXctestLauncher(proc process.Runner, clock clock.Clock, cfg LaunchConfig) *XctestLauncher {
	if cfg.Port == 0 {
		cfg.Port = defaultIosPort
	}
	if cfg.BuildTimeoutMs == 0 {
		cfg.BuildTimeoutMs = xctestDefaultBuildTimeoutMs
	}
	if cfg.LaunchTimeoutMs == 0 {
		cfg.LaunchTimeoutMs = xctestDefaultLaunchTimeoutMs
	}
	return &XctestLauncher{proc: proc, clock: clock, cfg: cfg}
}

// noopKill is the kill function EnsureRunning returns whenever it
// returns without having spawned a process of its own: an early
// error, or the probe-already-answers short circuit where some other
// caller (or a prior EnsureRunning call) owns the running driver's
// lifecycle instead.
func noopKill() error { return nil }

// EnsureRunning makes sure the XCUITest driver answers a ping
// handshake on cfg.Port, building and spawning it first when
// necessary: a driver already answering the handshake short-circuits
// before touching the filesystem or spawning any process; otherwise
// it builds only when the compiled test runner is missing or older
// than the project file or a Swift source file, then spawns and
// blocks until the driver answers the handshake or a failure mode
// below is confirmed. It is idempotent and safe to call repeatedly.
//
// kill is never nil, on either a nil or a non-nil error: calling it
// stops the xcodebuild process this call spawned, so a caller must
// arrange for kill to run once the driver is no longer needed, or the
// spawned process (and the XCUITest runner tree it owns) outlives the
// caller with nothing left able to stop it. When EnsureRunning did
// not spawn a process itself, kill is a no-op.
//
// A non-nil error carries an *XctestLauncherError except when ctx is
// canceled or the poll loop's own Sleep call fails, in which case the
// ctx error is returned directly.
func (l *XctestLauncher) EnsureRunning(ctx context.Context) (kill func() error, err error) {
	if err := ctx.Err(); err != nil {
		return noopKill, err
	}
	if probeIproxyPing(ctx, l.cfg.Port, iproxyPingProbeTimeout) {
		return noopKill, nil
	}

	projectFileName := l.cfg.projectFileName()
	xcodeprojPath := filepath.Join(l.cfg.ProjectDir, projectFileName)
	if _, err := os.Stat(xcodeprojPath); err != nil {
		return noopKill, &XctestLauncherError{
			Code: XctestErrNoProject,
			Message: fmt.Sprintf(
				"%s not found at %s. Run `cd %s && make setup` first.",
				projectFileName, xcodeprojPath, l.cfg.ProjectDir,
			),
		}
	}

	buildConfig := xctestConfigSimulator
	if l.cfg.Kind == KindDevice {
		buildConfig = xctestConfigDevice
	}
	runnerApp := filepath.Join(l.cfg.ProjectDir, "build", "Build", "Products", buildConfig, l.cfg.runnerAppName())
	derivedDataPath := filepath.Join(l.cfg.ProjectDir, "build")

	if l.needsBuild(runnerApp) {
		if err := l.build(ctx, xcodeprojPath, derivedDataPath); err != nil {
			return noopKill, err
		}
	}

	args, err := l.spawnArgs(xcodeprojPath, derivedDataPath)
	if err != nil {
		return noopKill, err
	}
	return l.spawnAndWait(ctx, args)
}

// needsBuild reports whether runnerApp is missing, or older than the
// project file or a Swift source file under the project directory.
func (l *XctestLauncher) needsBuild(runnerApp string) bool {
	info, err := os.Stat(runnerApp)
	if err != nil {
		return true
	}
	return l.isSourceNewer(info.ModTime())
}

// isSourceNewer reports whether project.pbxproj or any .swift file
// under xctestSourceDirNames is newer than productMtime. A pbxproj
// regeneration (for example after a signing setting changes) must
// force a rebuild even when no .swift file changed, because the
// bundle identifier and signing settings baked into the compiled
// product come from the project file. A stat failure on a source
// directory (as opposed to the directory simply not existing) is
// treated as stale, since it leaves the true staleness unknown.
func (l *XctestLauncher) isSourceNewer(productMtime time.Time) bool {
	pbxproj := filepath.Join(l.cfg.ProjectDir, l.cfg.projectFileName(), "project.pbxproj")
	if info, err := os.Stat(pbxproj); err == nil && info.ModTime().After(productMtime) {
		return true
	}

	for _, name := range xctestSourceDirNames {
		dir := filepath.Join(l.cfg.ProjectDir, name)
		newer, err := dirHasSwiftFileNewerThan(dir, productMtime)
		if err != nil {
			return true
		}
		if newer {
			return true
		}
	}
	return false
}

// dirHasSwiftFileNewerThan reports whether dir contains a .swift file
// modified after cutoff. A missing dir is not stale; any other stat
// or walk failure is surfaced so the caller can treat it as stale.
func dirHasSwiftFileNewerThan(dir string, cutoff time.Time) (bool, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".swift") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(cutoff) {
			found = true
		}
		return nil
	})
	return found, err
}

// destination builds the xcodebuild -destination value: a physical
// device targets the "iOS" platform, a simulator the "iOS Simulator"
// platform, both keyed by cfg.UDID.
func (l *XctestLauncher) destination() string {
	if l.cfg.Kind == KindDevice {
		return fmt.Sprintf("platform=iOS,id=%s", l.cfg.UDID)
	}
	return fmt.Sprintf("platform=iOS Simulator,id=%s", l.cfg.UDID)
}

// signOverrides builds the xcodebuild signing-setting overrides. A
// simulator build zeroes every signing setting, because the
// .xcodeproj may have been generated with physical-device signing
// baked in and the simulator does not use provisioning profiles at
// all. A physical device requires cfg.DevTeam; without it, no build
// or spawn command is ever constructed.
func (l *XctestLauncher) signOverrides() ([]string, error) {
	if l.cfg.Kind != KindDevice {
		return []string{
			"CODE_SIGN_STYLE=Automatic",
			"CODE_SIGNING_REQUIRED=NO",
			"CODE_SIGNING_ALLOWED=NO",
			"CODE_SIGN_IDENTITY=",
			"PROVISIONING_PROFILE_SPECIFIER=",
			"DEVELOPMENT_TEAM=",
		}, nil
	}
	if l.cfg.DevTeam == "" {
		return nil, &XctestLauncherError{
			Code:    XctestErrSigning,
			Message: "devTeam is required for physical device builds. Set ATOMYX_DEV_TEAM env var or pass devTeam option.",
		}
	}
	return []string{
		"CODE_SIGN_STYLE=Automatic",
		"CODE_SIGNING_REQUIRED=YES",
		"CODE_SIGNING_ALLOWED=YES",
		fmt.Sprintf("DEVELOPMENT_TEAM=%s", l.cfg.DevTeam),
	}, nil
}

// buildArgs and spawnArgs share every flag up to the command verb and
// the presence of -only-testing on the spawn side. process.Runner
// carries no working-directory concept the way execFile's cwd option
// does, so xcodeprojPath and derivedDataPath are absolute paths
// rooted at cfg.ProjectDir rather than the cwd-relative
// "AtomyxDriver.xcodeproj" and "./build" a subprocess spawned with
// cwd set to the project directory would otherwise resolve.

func (l *XctestLauncher) buildArgs(xcodeprojPath, derivedDataPath string) ([]string, error) {
	signs, err := l.signOverrides()
	if err != nil {
		return nil, err
	}
	args := []string{
		"build-for-testing",
		"-project", xcodeprojPath,
		"-scheme", l.cfg.scheme(),
		"-destination", l.destination(),
		"-derivedDataPath", derivedDataPath,
	}
	return append(args, signs...), nil
}

func (l *XctestLauncher) spawnArgs(xcodeprojPath, derivedDataPath string) ([]string, error) {
	signs, err := l.signOverrides()
	if err != nil {
		return nil, err
	}
	args := []string{
		"test-without-building",
		"-project", xcodeprojPath,
		"-scheme", l.cfg.scheme(),
		"-destination", l.destination(),
		"-derivedDataPath", derivedDataPath,
		"-only-testing:" + l.cfg.onlyTestingTarget(),
	}
	return append(args, signs...), nil
}

// build runs "xcodebuild build-for-testing" through the injected
// Runner and classifies a failure into a named XctestLauncherError: a
// missing xcodebuild binary, a signing-specific stderr pattern, or a
// generic build failure carrying a stderr excerpt.
func (l *XctestLauncher) build(ctx context.Context, xcodeprojPath, derivedDataPath string) error {
	args, err := l.buildArgs(xcodeprojPath, derivedDataPath)
	if err != nil {
		return err
	}

	buildCtx, cancel := context.WithTimeout(ctx, time.Duration(l.cfg.BuildTimeoutMs)*time.Millisecond)
	defer cancel()

	result, runErr := l.proc.Run(buildCtx, "xcodebuild", args...)
	if runErr == nil {
		return nil
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return &XctestLauncherError{Code: XctestErrNoXcode, Message: "xcodebuild not found. Install Xcode from the App Store."}
	}
	if isSigningFailure(result.Stderr) {
		return &XctestLauncherError{
			Code: XctestErrSigning,
			Message: fmt.Sprintf(
				"Xcode code signing not configured. Set ATOMYX_DEV_TEAM env var or pass devTeam to IosDriverOptions.\n\n"+
					"xcodebuild stderr (last %d chars):\n%s",
				xctestSigningStderrTailChars, tailString(result.Stderr, xctestSigningStderrTailChars),
			),
		}
	}
	tail := tailString(result.Stderr, xctestBuildFailedStderrTailChars)
	if tail == "" {
		tail = "(no stderr)"
	}
	return &XctestLauncherError{
		Code: XctestErrBuildFailed,
		Message: fmt.Sprintf(
			"xcodebuild build-for-testing failed (exit code %d).\nRun `cd %s && make setup` to diagnose.\n\n"+
				"stderr (last %d chars):\n%s",
			result.ExitCode, l.cfg.ProjectDir, xctestBuildFailedStderrTailChars, tail,
		),
	}
}

// isSigningFailure reports whether stderr from a failed xcodebuild
// invocation names a code-signing problem rather than a generic build
// failure.
func isSigningFailure(stderr string) bool {
	return strings.Contains(stderr, "requires a development team") ||
		strings.Contains(stderr, "No profiles were found") ||
		strings.Contains(stderr, "Signing for")
}

// tailString returns the last n bytes of s, or s unchanged when it is
// already n bytes or shorter.
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// spawnAndWait runs "xcodebuild test-without-building" through the
// injected Runner on its own goroutine, since it is a long-running
// process rather than a command that completes, and does not return
// until the driver answers the ping handshake or one of the
// XctestLauncherErrorCode failure modes is confirmed instead. Failing
// to become ready cancels the spawned process before returning, so a
// caller never observes success without a live driver behind it.
//
// On success, the returned kill function cancels runCtx and blocks
// until the goroutine above has observed the process exit, so a
// caller that calls kill can rely on the xcodebuild process (and the
// XCUITest runner tree it owns) being gone once kill returns. It is
// wrapped in sync.Once so a caller that invokes kill more than once
// does not block forever the second time on a channel the first call
// already drained.
//
// runCtx is deliberately rooted at context.Background() instead of
// ctx: the spawned driver process must keep running past this call
// returning, and past ctx being canceled, until the returned kill
// function stops it explicitly.
//
//nolint:contextcheck // see the comment above
func (l *XctestLauncher) spawnAndWait(ctx context.Context, args []string) (func() error, error) {
	runCtx, cancel := context.WithCancel(context.Background())
	exited := make(chan error, 1)
	go func() {
		result, runErr := l.proc.Run(runCtx, "xcodebuild", args...)
		exited <- classifySpawnExit(result, runErr)
	}()

	exitedAlready, err := l.waitForReadyOrExit(ctx, exited)
	if err != nil {
		cancel()
		// exited is a single-send buffered channel: once
		// waitForReadyOrExit has received from it, no further value
		// will ever arrive, so draining it again here would block
		// forever.
		if !exitedAlready {
			<-exited
		}
		return noopKill, err
	}
	var once sync.Once
	kill := func() error {
		once.Do(func() {
			cancel()
			<-exited
		})
		return nil
	}
	return kill, nil
}

// waitForReadyOrExit polls the ping handshake on cfg.Port every
// xctestPollIntervalMs, racing that poll against the spawned process
// exiting early and against ctx being canceled, until cfg.LaunchTimeoutMs
// elapses on the injected Clock. exitedAlready reports whether exited
// was the channel that resolved the wait, so the caller knows not to
// receive from it again.
func (l *XctestLauncher) waitForReadyOrExit(ctx context.Context, exited <-chan error) (exitedAlready bool, err error) {
	probe := func() (bool, error) {
		return probeIproxyPing(ctx, l.cfg.Port, iproxyPingProbeTimeout), nil
	}
	outcome, err := process.AwaitReady(ctx, l.clock, probe, exited, xctestPollIntervalMs, l.cfg.LaunchTimeoutMs)
	switch outcome {
	case process.ReadinessReady:
		return false, nil
	case process.ReadinessExited:
		return true, err
	case process.ReadinessTimeout:
		return false, &XctestLauncherError{
			Code: XctestErrLaunchTimeout,
			Message: fmt.Sprintf(
				"XCUITest driver did not start listening on port %d within %ds. Check Xcode console for errors.",
				l.cfg.Port, l.cfg.LaunchTimeoutMs/1000,
			),
		}
	default: // process.ReadinessCtxDone
		return false, err
	}
}

// classifySpawnExit turns a spawned "xcodebuild test-without-building"
// invocation's result, once it has exited before the driver became
// reachable, into a named XctestLauncherError.
func classifySpawnExit(result process.Result, runErr error) error {
	if runErr != nil && errors.Is(runErr, exec.ErrNotFound) {
		return &XctestLauncherError{Code: XctestErrNoXcode, Message: "xcodebuild not found. Install Xcode from the App Store."}
	}
	stderr := strings.TrimSpace(result.Stderr)
	if stderr == "" {
		stderr = "(empty)"
	} else {
		stderr = tailString(stderr, xctestSpawnExitStderrTailChars)
	}
	return &XctestLauncherError{
		Code: XctestErrSpawnExit,
		Message: fmt.Sprintf(
			"xcodebuild test-without-building exited early (code %d). stderr: %s",
			result.ExitCode, stderr,
		),
	}
}
