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
	"sync/atomic"
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
	// xctestSDKSimulator and xctestSDKDevice are the platform names
	// xcodebuild puts in the .xctestrun file name, after the scheme.
	xctestSDKSimulator = "iphonesimulator"
	xctestSDKDevice    = "iphoneos"
	xctestrunSuffix    = ".xctestrun"
	// xctestBuildLockName is the lock file inside the shared build
	// directory that serializes builds across launches and processes.
	xctestBuildLockName = ".build.lock"
	// xctestRunDirPrefix and xctestResultBundleName name each target's own
	// derived data directory and result bundle under the build directory.
	xctestRunDirPrefix     = "run-"
	xctestResultBundleName = "runner.xcresult"
)

// Timing defaults and the poll cadence EnsureRunning uses while
// waiting for a freshly spawned driver to answer the ping handshake.
const (
	// DefaultBuildTimeoutMs bounds one runner build when the launch
	// configuration sets none.
	DefaultBuildTimeoutMs        int64 = 300_000
	xctestDefaultLaunchTimeoutMs int64 = 90_000
	xctestPollIntervalMs         int64 = 500
	// xctestBuildLockPollMs is the pause between attempts to take the
	// shared build lock while another launch holds it.
	xctestBuildLockPollMs int64 = 250
)

// Progress messages the launcher reports through LaunchConfig.Progress.
const (
	XctestProgressBuilding = "building runner"
	XctestProgressStarting = "starting runner"
)

// Launch retry policy. The host port is chosen by probing for a free
// one, which races against the real bind, so a launch that loses that
// race is retried on a fresh port a bounded number of times.
const (
	// XctestMaxLaunchAttempts bounds the allocate-and-launch sequence.
	XctestMaxLaunchAttempts = 3
	// xcodebuildExitOccupiedPort is the exit status xcodebuild reports
	// when the runner could not bind its port, which is a retryable
	// failure. xcodebuild uses the same status for a failing test, so it
	// is only retried while a port allocator is configured.
	xcodebuildExitOccupiedPort = 65
)

// Runner launch environment. xcodebuild forwards every
// TEST_RUNNER_<NAME> variable to the test runner as <NAME>, which the
// Swift runner reads at startup.
const (
	xctestEnvPort       = "TEST_RUNNER_ATOMYX_PORT"
	xctestEnvRunnerID   = "TEST_RUNNER_ATOMYX_RUNNER_ID"
	xctestEnvTargetUDID = "TEST_RUNNER_ATOMYX_TARGET_UDID"
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
	// XctestErrRunnerMismatch means a runner answered on the port but
	// is not the one this launch expects.
	XctestErrRunnerMismatch XctestLauncherErrorCode = "runner-mismatch"
)

// XctestLauncherError is a named, actionable failure from
// XctestLauncher.EnsureRunning, so a caller can branch on Code
// instead of matching Message.
type XctestLauncherError struct {
	Code    XctestLauncherErrorCode
	Message string
	// ExitCode is the xcodebuild exit status for XctestErrSpawnExit,
	// and zero for every other code.
	ExitCode int
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
	// CodeSignStyle is the xcodebuild CODE_SIGN_STYLE for a physical
	// device build, either "Automatic" or "Manual". Empty means
	// Automatic. The caller resolves any ATOMYX_CODE_SIGN_STYLE
	// environment fallback before constructing this config.
	CodeSignStyle string
	// ProvisioningProfileSpecifier names the provisioning profile used
	// for a Manual signing device build. Required when CodeSignStyle is
	// Manual and ignored otherwise. The caller resolves any
	// ATOMYX_PROVISIONING_PROFILE_SPECIFIER environment fallback before
	// constructing this config.
	ProvisioningProfileSpecifier string
	// RunnerAppName is the compiled XCUITest runner product's file
	// name under the build output directory. Defaults to the standard
	// Atomyx driver runner app name when empty.
	RunnerAppName string
	// OnlyTestingTarget is the -only-testing selector xcodebuild uses
	// to run just the XCUITest driver's command-serving test. Defaults
	// to the standard Atomyx driver testing target when empty.
	OnlyTestingTarget string
	// BuildTimeoutMs bounds a single "xcodebuild build-for-testing"
	// invocation, excluding the wait for the shared build lock. Defaults
	// to 300000 (300s) when zero, because a fresh machine compiles the
	// Swift runner from scratch.
	BuildTimeoutMs int64
	// LaunchTimeoutMs bounds the wait for the spawned driver to start
	// answering the ping handshake. Defaults to 90000 (90s) when zero.
	LaunchTimeoutMs int64
	// RunnerID is the launch identifier handed to the runner. The
	// launcher accepts only a runner that echoes it back. Empty means
	// identity rests on the UDID alone.
	RunnerID string
	// AllocatePort, when set, picks a fresh host port for every launch
	// attempt instead of using Port, and enables the bounded retry on a
	// lost bind race. XctestLauncher.Port reports the port the last
	// attempt used.
	AllocatePort func() (int, error)
	// ReleasePort receives the host port of every launch attempt that
	// failed, as soon as the attempt has been torn down, so a caller that
	// reserved the port frees it before the next attempt or the final
	// error. Nil ignores it.
	ReleasePort func(port int)
	// Warn receives a one-line warning when a runner is adopted without
	// a launch identifier. Nil discards it.
	Warn func(message string)
	// RunnerPort is the port the runner binds inside its own network
	// namespace. Zero means the host port for a simulator, which shares
	// the host loopback, and the fixed device-side port 22087 for a
	// physical device, which an iproxy tunnel maps the host port onto.
	RunnerPort int
	// AfterSpawn runs after the runner process has started and before
	// the wait for its ping answer, on every launch attempt. It starts
	// whatever the runner needs to be reachable on the host port, such
	// as the USB tunnel to a physical device, and returns the function
	// that tears that down. A non-nil error fails the attempt and stops
	// the runner. The returned stop function, when non-nil, runs after
	// the runner has been stopped, on a failed attempt and on kill alike.
	AfterSpawn func(ctx context.Context, spawned SpawnedRunner) (stop func(), err error)
	// OnRunnerExit receives the verdict when the runner process exits on
	// its own after it answered ready, once per launch. A stop through the
	// returned kill function is not reported. Nil watches nothing.
	OnRunnerExit func(verdict error)
	// Progress receives XctestProgressBuilding when a build is about to
	// run and XctestProgressStarting before the runner launch. Nil
	// discards it.
	Progress func(message string)
}

// SpawnedRunner identifies a runner process the launcher has started
// and not yet confirmed ready.
type SpawnedRunner struct {
	// HostPort is the host port the launcher will probe for the ping
	// answer.
	HostPort int
	// RunnerPort is the port the runner binds in its own namespace.
	RunnerPort int
	// Pid is the process id of the xcodebuild process, which leads its
	// own process group.
	Pid int
	// Product identifies the compiled runner product the process was
	// started from. It is the zero value when the product cannot be read.
	Product BuildProduct
}

// BuildProduct identifies one compiled runner product. A build records the
// digest of its inputs next to the product and stamps the product's
// modification time, so a rebuilt product never shares its identity with
// the one it replaced.
type BuildProduct struct {
	// Path is the location of the compiled runner app.
	Path string
	// ModTime is the product's modification time in UTC.
	ModTime time.Time
	// Digest is the digest of the inputs the product was built from. It
	// is empty for a product built before digests were recorded.
	Digest string
}

// Matches reports whether other names the same product at the same
// build. Two products at the same path match when both carry a digest
// and the digests are equal. When either has no digest, they match when
// their modification times are equal. A zero product matches nothing.
func (p BuildProduct) Matches(other BuildProduct) bool {
	if p.Path == "" || p.Path != other.Path {
		return false
	}
	if p.Digest != "" && other.Digest != "" {
		return p.Digest == other.Digest
	}
	return p.ModTime.Equal(other.ModTime)
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
	proc       process.Runner
	background process.BackgroundRunner
	clock      clock.Clock
	cfg        LaunchConfig
	port       int
	xcode      xcodeIdentityCache
}

// Port reports the host port the most recent EnsureRunning attempt
// used. It equals the configured port unless LaunchConfig.AllocatePort
// is set.
func (l *XctestLauncher) Port() int { return l.port }

func (l *XctestLauncher) expected() ExpectedRunner {
	return ExpectedRunner{UDID: l.cfg.UDID, Kind: l.cfg.Kind, RunnerID: l.cfg.RunnerID}
}

// NewXctestLauncher constructs an XctestLauncher. proc, background,
// and clock are injected so a test can substitute fakes for the
// bounded "build-for-testing" invocation, the long-lived
// "test-without-building" process, and the poll timing, without a
// real Xcode toolchain or device. A nil background panics, because
// there is no safe default for a runner that spawns real processes.
func NewXctestLauncher(proc process.Runner, background process.BackgroundRunner, clock clock.Clock, cfg LaunchConfig) *XctestLauncher {
	if background == nil {
		panic("iosdriver.NewXctestLauncher: background runner is required")
	}
	if cfg.Port == 0 {
		cfg.Port = defaultIosPort
	}
	if cfg.BuildTimeoutMs == 0 {
		cfg.BuildTimeoutMs = DefaultBuildTimeoutMs
	}
	if cfg.LaunchTimeoutMs == 0 {
		cfg.LaunchTimeoutMs = xctestDefaultLaunchTimeoutMs
	}
	return &XctestLauncher{proc: proc, background: background, clock: clock, cfg: cfg, port: cfg.Port}
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
//
// A fresh product launches without taking the build lock, so a launch
// never waits on another process's build of a different configuration.
// Only a missing or stale product takes the lock and builds.
func (l *XctestLauncher) EnsureRunning(ctx context.Context) (kill func() error, err error) {
	if err := ctx.Err(); err != nil {
		return noopKill, err
	}
	if l.cfg.AllocatePort == nil {
		got, found, unmanaged, idErr := CheckRunner(ctx, l.cfg.Port, iproxyPingProbeTimeout, l.expected())
		if idErr != nil {
			return noopKill, runnerMismatchError(idErr)
		}
		if found {
			if !got.SpeaksHostProtocol() {
				return noopKill, runnerProtocolMismatchError(l.cfg.Port, got.ProtocolVersion)
			}
			if unmanaged && l.cfg.Warn != nil {
				l.cfg.Warn(unmanagedRunnerWarning(l.cfg.Port))
			}
			return noopKill, nil
		}
	}

	if err := l.ensureBuilt(ctx, false); err != nil {
		return noopKill, err
	}
	buildDir := l.buildDir()
	xctestrun, err := l.findXctestrun(buildDir)
	if err != nil {
		return noopKill, err
	}

	l.progress(XctestProgressStarting)
	return l.launchWithRetry(ctx, xctestrun, filepath.Join(buildDir, xctestRunDirPrefix+l.cfg.UDID))
}

// EnsureBuilt makes sure the compiled test runner exists and matches its
// current inputs, building it when it is missing or stale. It does not start the
// runner. The build is serialized across launches and processes by a
// lock on the shared build directory, so a call that waits for another process's build
// returns once that build finishes, and builds nothing when the
// product is then fresh. It returns an *XctestLauncherError for a
// missing project, a signing problem or a failed build, and ctx's
// error when ctx ends while waiting for the lock.
func (l *XctestLauncher) EnsureBuilt(ctx context.Context) error {
	return l.ensureBuilt(ctx, true)
}

// ensureBuilt validates the project, then builds the runner when it is
// missing or stale. With alwaysLock false, a product that is already
// fresh returns without touching the build lock.
func (l *XctestLauncher) ensureBuilt(ctx context.Context, alwaysLock bool) error {
	projectFileName := l.cfg.projectFileName()
	xcodeprojPath := filepath.Join(l.cfg.ProjectDir, projectFileName)
	if _, err := os.Stat(xcodeprojPath); err != nil {
		return &XctestLauncherError{
			Code: XctestErrNoProject,
			Message: fmt.Sprintf(
				"%s not found at %s. Run `cd %s && make setup` first.",
				projectFileName, xcodeprojPath, l.cfg.ProjectDir,
			),
		}
	}
	if _, err := l.signOverrides(); err != nil {
		return err
	}
	buildDir := l.buildDir()
	runnerApp := l.runnerAppPath()
	if !alwaysLock && !l.needsBuild(ctx, runnerApp) {
		return nil
	}
	return l.buildLocked(ctx, xcodeprojPath, buildDir, runnerApp)
}

func (l *XctestLauncher) buildDir() string { return filepath.Join(l.cfg.ProjectDir, "build") }

// runnerAppPath is the compiled runner product for this launch's platform.
func (l *XctestLauncher) runnerAppPath() string {
	buildConfig := xctestConfigSimulator
	if l.cfg.Kind == KindDevice {
		buildConfig = xctestConfigDevice
	}
	return filepath.Join(l.buildDir(), "Build", "Products", buildConfig, l.cfg.runnerAppName())
}

// CurrentBuildProduct identifies the compiled runner product when it
// exists and is current. A product with a recorded digest is current
// when the digest of its inputs is unchanged, and a product with none is
// current when it is not older than the project file or a Swift source.
// It builds nothing. ok is false for a missing or stale product, because
// a runner started from such a product does not run the current source. ctx
// bounds the commands that read the selected Xcode.
func (l *XctestLauncher) CurrentBuildProduct(ctx context.Context) (product BuildProduct, ok bool) {
	product, ok = l.statProduct()
	if !ok || l.needsBuild(ctx, product.Path) {
		return BuildProduct{}, false
	}
	return product, true
}

func (l *XctestLauncher) statProduct() (BuildProduct, bool) {
	path := l.runnerAppPath()
	info, err := os.Stat(path)
	if err != nil {
		return BuildProduct{}, false
	}
	return BuildProduct{Path: path, ModTime: info.ModTime().UTC(), Digest: readDigest(path)}, true
}

func (l *XctestLauncher) progress(message string) {
	if l.cfg.Progress != nil {
		l.cfg.Progress(message)
	}
}

// buildLocked builds the runner into buildDir when it is missing or
// stale. The shared build directory is serialized by a lock on
// buildDir/.build.lock, and the decision to build is taken only after
// the lock is held, so launches that start together build exactly once
// and never race on the same derived data. A launch that waited on the
// lock finds the product the holder built and skips the build.
func (l *XctestLauncher) buildLocked(ctx context.Context, xcodeprojPath, buildDir, runnerApp string) error {
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("iosdriver: create the build directory: %w", err)
	}
	release, err := acquireBuildLock(ctx, l.clock, filepath.Join(buildDir, xctestBuildLockName))
	if err != nil {
		return err
	}
	defer release()
	if !l.needsBuild(ctx, runnerApp) {
		return nil
	}
	// The digest is taken before the build, so a source edited while the
	// build runs changes the next digest and forces another build. The
	// old digest goes first, so a build that fails or is interrupted
	// leaves a product that claims no inputs.
	digest, digestErr := l.buildDigest(ctx)
	removeDigest(runnerApp)
	l.progress(XctestProgressBuilding)
	buildStarted := time.Now()
	if err := l.build(ctx, xcodeprojPath, buildDir); err != nil {
		return err
	}
	// A build that finds nothing to recompile leaves the product's
	// modification time alone, so sources touched without a content
	// change would keep the product looking stale and every later launch
	// would rebuild. Stamping the product records that the build has
	// seen those sources. The stamp is the time the build started, not
	// the time it ended, because a source edited while the build ran may
	// not be in the product and must still read as newer than it. A
	// failure to stamp only costs one redundant build on the next launch,
	// so it is not reported.
	_ = os.Chtimes(runnerApp, buildStarted, buildStarted)
	// A digest that cannot be computed or recorded leaves the product
	// without one, which falls back to the modification time check.
	if digestErr == nil {
		_ = writeDigest(runnerApp, digest)
	}
	return nil
}

// findXctestrun returns the newest .xctestrun file the build wrote for
// this launch's platform. Every target runs from that one file, with
// its own derived data path, so no target writes into the shared build
// directory.
func (l *XctestLauncher) findXctestrun(buildDir string) (string, error) {
	sdk := xctestSDKSimulator
	if l.cfg.Kind == KindDevice {
		sdk = xctestSDKDevice
	}
	productsDir := filepath.Join(buildDir, "Build", "Products")
	prefix := l.cfg.scheme() + "_" + sdk
	entries, err := os.ReadDir(productsDir)
	if err != nil {
		return "", &XctestLauncherError{
			Code:    XctestErrBuildFailed,
			Message: fmt.Sprintf("the runner build left no products under %s: %v. Run `cd %s && make setup` to diagnose.", productsDir, err, l.cfg.ProjectDir),
		}
	}
	var best string
	var bestTime time.Time
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, xctestrunSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) || (info.ModTime().Equal(bestTime) && name > best) {
			best, bestTime = name, info.ModTime()
		}
	}
	if best == "" {
		return "", &XctestLauncherError{
			Code: XctestErrBuildFailed,
			Message: fmt.Sprintf("the runner build wrote no %s*%s file under %s. Run `cd %s && make setup` to diagnose.",
				prefix, xctestrunSuffix, productsDir, l.cfg.ProjectDir),
		}
	}
	return filepath.Join(productsDir, best), nil
}

// launchWithRetry spawns the runner and waits for it. With an
// AllocatePort configured, each attempt takes a fresh port, and a
// failure that means the port was taken (an early xcodebuild exit with
// xcodebuildExitOccupiedPort, or another runner answering on it) is
// retried until XctestMaxLaunchAttempts attempts have run. Without an
// allocator the port is fixed, so a retry could not change the outcome
// and the first failure is returned.
func (l *XctestLauncher) launchWithRetry(ctx context.Context, xctestrun, runDerived string) (func() error, error) {
	for attempt := 1; ; attempt++ {
		port := l.cfg.Port
		if l.cfg.AllocatePort != nil {
			allocated, allocErr := l.cfg.AllocatePort()
			if allocErr != nil {
				return noopKill, fmt.Errorf("iosdriver: allocate runner port: %w", allocErr)
			}
			port = allocated
		}
		l.port = port

		args := l.spawnArgs(xctestrun, runDerived)
		kill, err := l.spawnAndWait(ctx, port, runDerived, args)
		if err == nil {
			return kill, nil
		}
		if l.cfg.AllocatePort != nil && l.cfg.ReleasePort != nil {
			l.cfg.ReleasePort(port)
		}
		if l.cfg.AllocatePort == nil || attempt >= XctestMaxLaunchAttempts || !isPortConflictFailure(err) {
			return noopKill, err
		}
	}
}

// isPortConflictFailure reports whether err means the launch lost the
// race for its host port.
func isPortConflictFailure(err error) bool {
	var tunnelErr *IproxyError
	if errors.As(err, &tunnelErr) {
		return tunnelErr.Code == IproxyErrBindConflict
	}
	var launchErr *XctestLauncherError
	if !errors.As(err, &launchErr) {
		return false
	}
	switch launchErr.Code {
	case XctestErrRunnerMismatch:
		return true
	case XctestErrSpawnExit:
		return launchErr.ExitCode == xcodebuildExitOccupiedPort
	default:
		return false
	}
}

func runnerMismatchError(err *RunnerIdentityError) *XctestLauncherError {
	return &XctestLauncherError{Code: XctestErrRunnerMismatch, Message: err.Error()}
}

// needsBuild reports whether runnerApp is missing or stale. A product
// with a recorded digest is stale when the digest of its inputs differs,
// or cannot be computed. A product with no recorded digest is stale when
// it is older than the project file or a Swift source file under the
// project directory, so a product built before digests were recorded is
// not rebuilt on that account alone.
func (l *XctestLauncher) needsBuild(ctx context.Context, runnerApp string) bool {
	info, err := os.Stat(runnerApp)
	if err != nil {
		return true
	}
	recorded := readDigest(runnerApp)
	if recorded == "" {
		return l.isSourceNewer(info.ModTime())
	}
	current, err := l.buildDigest(ctx)
	return err != nil || current != recorded
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

const (
	codeSignStyleAutomatic = "Automatic"
	codeSignStyleManual    = "Manual"
)

// signOverrides builds the xcodebuild signing-setting overrides. A
// simulator build zeroes every signing setting, because the
// .xcodeproj may have been generated with physical-device signing
// baked in and the simulator does not use provisioning profiles at
// all. A physical device requires cfg.DevTeam, and a Manual signing
// style also requires cfg.ProvisioningProfileSpecifier. Without them,
// no build or spawn command is ever constructed.
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
	style := l.cfg.CodeSignStyle
	if style == "" {
		style = codeSignStyleAutomatic
	}
	overrides := []string{
		"CODE_SIGN_STYLE=" + style,
		"CODE_SIGNING_REQUIRED=YES",
		"CODE_SIGNING_ALLOWED=YES",
		fmt.Sprintf("DEVELOPMENT_TEAM=%s", l.cfg.DevTeam),
	}
	switch style {
	case codeSignStyleAutomatic:
		return overrides, nil
	case codeSignStyleManual:
		if l.cfg.ProvisioningProfileSpecifier == "" {
			return nil, &XctestLauncherError{
				Code: XctestErrSigning,
				Message: "Manual code signing requires a provisioning profile. " +
					"Set ATOMYX_PROVISIONING_PROFILE_SPECIFIER to the profile name, " +
					"or set ATOMYX_CODE_SIGN_STYLE=Automatic.",
			}
		}
		return append(overrides, "PROVISIONING_PROFILE_SPECIFIER="+l.cfg.ProvisioningProfileSpecifier), nil
	default:
		return nil, &XctestLauncherError{
			Code: XctestErrSigning,
			Message: fmt.Sprintf(
				"Unsupported code signing style %q. Set ATOMYX_CODE_SIGN_STYLE to Automatic or Manual.", style),
		}
	}
}

// process.Runner carries no working-directory concept the way
// execFile's cwd option does, so every path handed to xcodebuild is
// absolute and rooted at cfg.ProjectDir rather than the cwd-relative
// "AtomyxDriver.xcodeproj" and "./build" a subprocess spawned with cwd
// set to the project directory would otherwise resolve.

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

// spawnArgs runs the already built products from the shared build
// directory through xctestrun, with this target's own derived data path
// and result bundle, so concurrent targets never write to the same
// location.
func (l *XctestLauncher) spawnArgs(xctestrun, runDerived string) []string {
	return []string{
		"test-without-building",
		"-xctestrun", xctestrun,
		"-destination", l.destination(),
		"-derivedDataPath", runDerived,
		"-resultBundlePath", filepath.Join(runDerived, xctestResultBundleName),
		"-only-testing:" + l.cfg.onlyTestingTarget(),
	}
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

// spawnAndWait starts "xcodebuild test-without-building" through the
// injected BackgroundRunner, since it is a long-running process
// rather than a command that completes (Runner.Run would kill it at
// its fallback timeout), and does not return until the driver answers
// the ping handshake on port or one of the XctestLauncherErrorCode
// failure modes is confirmed instead. Failing to become ready kills
// the spawned process before returning, so a caller never observes
// success without a live driver behind it.
//
// The launch environment travels on the context, because xcodebuild
// forwards only TEST_RUNNER_ variables taken from its own environment.
// When LaunchConfig.AfterSpawn is set it runs once the process has
// started and before the ping wait.
//
// On success, the returned kill function kills the process group and
// blocks until the process has exited, then runs the AfterSpawn stop
// function, so a caller that calls kill can rely on the xcodebuild
// process (and the XCUITest runner tree it owns) being gone once kill
// returns. It is safe to call more than once.
//
// The process is started on a context detached from ctx: the spawned
// driver must keep running past this call returning, and past ctx
// being canceled, until the returned kill function stops it
// explicitly.
func (l *XctestLauncher) spawnAndWait(ctx context.Context, port int, runDerived string, args []string) (func() error, error) {
	if err := os.RemoveAll(filepath.Join(runDerived, xctestResultBundleName)); err != nil {
		return noopKill, fmt.Errorf("iosdriver: clear the previous result bundle: %w", err)
	}
	if err := os.MkdirAll(runDerived, 0o755); err != nil {
		return noopKill, fmt.Errorf("iosdriver: create the runner derived data directory: %w", err)
	}
	runnerPort := l.runnerPort(port)
	startCtx := process.WithEnv(context.WithoutCancel(ctx), l.runnerEnv(runnerPort)...)
	proc, startErr := l.background.Start(startCtx, "xcodebuild", args...)
	if startErr != nil {
		return noopKill, classifySpawnExit(process.BackgroundResult{}, startErr)
	}
	exited, stopRunner := superviseBackground(proc, classifySpawnExit)

	teardown := stopRunner
	if l.cfg.AfterSpawn != nil {
		product, _ := l.statProduct()
		stopHook, err := l.cfg.AfterSpawn(ctx, SpawnedRunner{HostPort: port, RunnerPort: runnerPort, Pid: proc.Pid(), Product: product})
		if err != nil {
			stopRunner()
			return noopKill, err
		}
		if stopHook != nil {
			teardown = func() {
				stopRunner()
				stopHook()
			}
		}
	}
	var stopping atomic.Bool
	stopAll := teardown
	teardown = onceFunc(func() {
		stopping.Store(true)
		stopAll()
	})

	if err := l.waitForReadyOrExit(ctx, port, exited); err != nil {
		teardown()
		return noopKill, err
	}
	notifyOnExit(exited, &stopping, l.cfg.OnRunnerExit)
	return func() error {
		teardown()
		return nil
	}, nil
}

func onceFunc(f func()) func() {
	var once sync.Once
	return func() { once.Do(f) }
}

// runnerPort returns the port the runner binds in its own namespace. A
// device runner always listens on deviceRunnerPort inside the device,
// and iproxy maps the host port onto it, so only a simulator runner
// binds the host port itself.
func (l *XctestLauncher) runnerPort(hostPort int) int {
	switch {
	case l.cfg.RunnerPort != 0:
		return l.cfg.RunnerPort
	case l.cfg.Kind == KindDevice:
		return deviceRunnerPort
	default:
		return hostPort
	}
}

// runnerEnv returns the TEST_RUNNER_ variables for a launch. xcodebuild
// forwards each of them to the runner without the prefix.
func (l *XctestLauncher) runnerEnv(runnerPort int) []string {
	env := []string{fmt.Sprintf("%s=%d", xctestEnvPort, runnerPort)}
	if l.cfg.RunnerID != "" {
		env = append(env, xctestEnvRunnerID+"="+l.cfg.RunnerID)
	}
	if l.cfg.Kind == KindDevice {
		env = append(env, xctestEnvTargetUDID+"="+l.cfg.UDID)
	}
	return env
}

// waitForReadyOrExit polls the ping handshake on port every
// xctestPollIntervalMs, racing that poll against the spawned process
// exiting early and against ctx being canceled, until cfg.LaunchTimeoutMs
// elapses on the injected Clock. A runner that answers but is not the
// expected one ends the wait with XctestErrRunnerMismatch, because
// that runner holds the port and the launched process never will.
//
// The exception is a physical device launched with an allocated host
// port: every host port maps onto the same device-side port, so an
// earlier runner of the same device that is still winding down answers
// first. That answer means the new runner is not ready yet, so the wait
// continues, and a timeout then names the earlier runner as the cause.
func (l *XctestLauncher) waitForReadyOrExit(ctx context.Context, port int, exited <-chan error) error {
	previousRunnerSeen := false
	probe := func() (bool, error) {
		found, _, idErr := CheckRunnerIdentity(ctx, port, iproxyPingProbeTimeout, l.expected())
		if idErr != nil {
			if l.waitsForPreviousRunner() && l.expected().IsPreviousDeviceRunner(idErr.Got) {
				previousRunnerSeen = true
				return false, nil
			}
			return false, runnerMismatchError(idErr)
		}
		return found, nil
	}
	outcome, err := process.AwaitReady(ctx, l.clock, probe, exited, xctestPollIntervalMs, l.cfg.LaunchTimeoutMs)
	switch outcome {
	case process.ReadinessReady, process.ReadinessExited:
		return err
	case process.ReadinessTimeout:
		if previousRunnerSeen {
			return &XctestLauncherError{
				Code: XctestErrLaunchTimeout,
				Message: fmt.Sprintf(
					"A previous runner of device %s still holds the device runner port, so the new runner did not become reachable within %ds. "+
						"Stop the earlier atomyx session or wait for it to exit, then try again.",
					l.cfg.UDID, l.cfg.LaunchTimeoutMs/1000,
				),
			}
		}
		return &XctestLauncherError{
			Code: XctestErrLaunchTimeout,
			Message: fmt.Sprintf(
				"XCUITest driver did not start listening on port %d within %ds. Check Xcode console for errors.",
				port, l.cfg.LaunchTimeoutMs/1000,
			),
		}
	default: // process.ReadinessCtxDone
		return err
	}
}

// waitsForPreviousRunner reports whether a runner of the same device
// with another launch id is treated as a predecessor to wait out. It
// holds only for a physical device with an allocated host port, because
// only there does every launch reach the one device-side runner port.
func (l *XctestLauncher) waitsForPreviousRunner() bool {
	return l.cfg.Kind == KindDevice && l.cfg.AllocatePort != nil
}

// classifySpawnExit turns a spawned "xcodebuild test-without-building"
// process outcome, once it has exited or failed to start before the driver became
// reachable, into a named XctestLauncherError.
func classifySpawnExit(result process.BackgroundResult, runErr error) error {
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
		Code:     XctestErrSpawnExit,
		ExitCode: result.ExitCode,
		Message: fmt.Sprintf(
			"xcodebuild test-without-building exited early (code %d). stderr: %s",
			result.ExitCode, stderr,
		),
	}
}
