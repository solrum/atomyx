package device

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// iosProjectDirWalkUpLimit bounds how many parent directories
// resolveIosProjectDir climbs from each anchor before giving up on that
// anchor. A bound this small already covers every workspace layout the
// runtime is embedded in (repo checkout, app bundle, nested monorepo);
// climbing further would only risk matching a platforms/ios-agent
// directory that belongs to an unrelated project higher up the tree.
const iosProjectDirWalkUpLimit = 8

// IosAgentKind selects the destination family EnsureIOSAgent builds
// and spawns the XCUITest driver against. Its values match the
// ensureIosAgent JSON-RPC method's "kind" enum ("simulator" |
// "device") byte for byte.
type IosAgentKind string

const (
	IosAgentKindSimulator IosAgentKind = "simulator"
	IosAgentKindDevice    IosAgentKind = "device"
)

// AgentProvisioner brings the on-device control-plane agent up for
// one iOS or Android device. It is the seam sidecar's ios-agent and
// android-agent features depend on so neither imports iosdriver or
// androiddriver directly. Only device, which already legitimately
// wires those driver families for Factory. A call blocks until the
// agent answers ready or a launcher failure is confirmed; the
// fire-and-forget contract callers see on the wire is built by
// running that call on a background goroutine, not by this
// interface.
type AgentProvisioner interface {
	// EnsureIOSAgent brings the XCUITest driver up for udid and blocks
	// until it answers ready or a launcher failure is confirmed. kill
	// is never nil: calling it stops the driver process this call
	// spawned (a no-op if this call spawned none, for example because
	// a driver was already running). The caller owns kill and must
	// invoke it once the agent is no longer needed, or the spawned
	// process outlives the caller with nothing left able to stop it.
	EnsureIOSAgent(ctx context.Context, udid string, kind IosAgentKind) (kill func() error, err error)
	EnsureAndroidAgent(ctx context.Context, serial string) error
}

// DriverProvisionerDeps configures a DriverProvisioner. A nil Clock
// defaults to orchestra.SystemClock{}. IosProjectDir, IosDevTeam,
// IosCodeSignStyle, IosProvisioningProfile, and AndroidAPKPath fall
// back to environment variables (ATOMYX_IOS_DRIVER_DIR,
// ATOMYX_DEV_TEAM, ATOMYX_CODE_SIGN_STYLE,
// ATOMYX_PROVISIONING_PROFILE_SPECIFIER, ATOMYX_ANDROID_APK) when
// left empty, so a caller that wants the
// production default need not resolve them itself.
type DriverProvisionerDeps struct {
	Runner process.Runner
	// Background starts the long-lived "xcodebuild
	// test-without-building" process the XCUITest launcher
	// supervises. It is required: NewDriverProvisioner panics when it
	// is nil.
	Background    process.BackgroundRunner
	Clock         orchestra.Clock
	IosProjectDir string
	IosDevTeam    string
	// IosCodeSignStyle is Automatic or Manual and IosProvisioningProfile
	// names the profile a Manual device build signs with.
	IosCodeSignStyle       string
	IosProvisioningProfile string
	AndroidAPKPath         string
	// IosScheme, IosProjectFile, IosRunnerApp, and IosOnlyTesting
	// override the corresponding Xcode project identifiers the
	// XCUITest launcher builds and spawns against. Each defaults to
	// the standard Atomyx driver identifier when empty.
	IosScheme      string
	IosProjectFile string
	IosRunnerApp   string
	IosOnlyTesting string
}

// DriverProvisioner is the production AgentProvisioner: each call
// constructs a fresh XctestLauncher or AgentLauncher and blocks on its
// EnsureRunning. It holds no per-device state of its own. Dedup and
// status tracking belong to the caller.
type DriverProvisioner struct {
	runner     process.Runner
	background process.BackgroundRunner
	clock      orchestra.Clock
	deps       DriverProvisionerDeps
}

// NewDriverProvisioner constructs a DriverProvisioner from deps.
func NewDriverProvisioner(deps DriverProvisionerDeps) *DriverProvisioner {
	clock := deps.Clock
	if clock == nil {
		clock = orchestra.SystemClock{}
	}
	background := deps.Background
	if background == nil {
		panic("device.NewDriverProvisioner: Background runner is required")
	}
	return &DriverProvisioner{runner: deps.Runner, background: background, clock: clock, deps: deps}
}

var _ AgentProvisioner = (*DriverProvisioner)(nil)

// resolveIosProjectDir finds the platforms/ios-agent directory the
// XCUITest launcher needs to build and run. Resolution tries three
// tiers in order and returns the first that settles the question:
//
//  1. configured, if non-empty: an explicit setting always wins,
//     because a caller that named a directory knows better than any
//     guess this function could make.
//  2. env, if non-empty and it names a path that exists on disk: the
//     deployment sets ATOMYX_IOS_DRIVER_DIR precisely because its
//     working directory does not sit near the project, so trust it
//     once it is confirmed to point somewhere real.
//  3. otherwise, walk upward from each of a fixed set of anchor
//     directories (the running executable's directory, then the
//     current working directory) looking for a platforms/ios-agent
//     subdirectory, bounded to iosProjectDirWalkUpLimit levels per
//     anchor. This covers the common case of a runtime binary spawned
//     from deep inside a checkout, with a cwd nowhere near the
//     platforms folder, and no environment variable set at all.
//
// An empty return means every tier was inconclusive. The caller must
// still construct a launcher with that empty value: the launcher owns
// reporting the resulting no-project-found error to its caller.
func resolveIosProjectDir(configured, env string) string {
	if configured != "" {
		return configured
	}
	if env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
	}

	var anchors []string
	if exe, err := os.Executable(); err == nil {
		anchors = append(anchors, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		anchors = append(anchors, cwd)
	}

	for _, anchor := range anchors {
		dir := anchor
		for i := 0; i < iosProjectDirWalkUpLimit; i++ {
			candidate := filepath.Join(dir, "platforms", "ios-agent")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// EnsureIOSAgent implements AgentProvisioner. It holds no per-device
// state of its own: the kill XctestLauncher.EnsureRunning returns is
// passed straight through, and tracking it against udid for a later
// teardown is the caller's job, per DriverProvisioner's own doc.
func (p *DriverProvisioner) EnsureIOSAgent(ctx context.Context, udid string, kind IosAgentKind) (func() error, error) {
	driverKind := iosdriver.KindSimulator
	if kind == IosAgentKindDevice {
		driverKind = iosdriver.KindDevice
	}
	projectDir := resolveIosProjectDir(p.deps.IosProjectDir, os.Getenv("ATOMYX_IOS_DRIVER_DIR"))
	devTeam := p.deps.IosDevTeam
	if devTeam == "" {
		devTeam = os.Getenv("ATOMYX_DEV_TEAM")
	}
	signStyle := p.deps.IosCodeSignStyle
	if signStyle == "" {
		signStyle = os.Getenv("ATOMYX_CODE_SIGN_STYLE")
	}
	profile := p.deps.IosProvisioningProfile
	if profile == "" {
		profile = os.Getenv("ATOMYX_PROVISIONING_PROFILE_SPECIFIER")
	}
	launcher := iosdriver.NewXctestLauncher(p.runner, p.background, p.clock, iosdriver.LaunchConfig{
		ProjectDir:                   projectDir,
		Port:                         iosControlPort,
		Kind:                         driverKind,
		UDID:                         udid,
		DevTeam:                      devTeam,
		CodeSignStyle:                signStyle,
		ProvisioningProfileSpecifier: profile,
		Scheme:                       p.deps.IosScheme,
		ProjectFileName:              p.deps.IosProjectFile,
		RunnerAppName:                p.deps.IosRunnerApp,
		OnlyTestingTarget:            p.deps.IosOnlyTesting,
	})
	return launcher.EnsureRunning(ctx)
}

// EnsureAndroidAgent implements AgentProvisioner.
func (p *DriverProvisioner) EnsureAndroidAgent(ctx context.Context, serial string) error {
	apkPath := p.deps.AndroidAPKPath
	if apkPath == "" {
		apkPath = os.Getenv("ATOMYX_ANDROID_APK")
	}
	client := transport.NewHTTPClient("127.0.0.1", androidControlPort)
	launcher := androiddriver.NewAgentLauncher(p.runner, p.clock, client, androiddriver.AgentLauncherOptions{
		Serial:     serial,
		HostPort:   androidControlPort,
		DevicePort: androidControlPort,
		APKPath:    apkPath,
	})
	return launcher.EnsureRunning(ctx)
}

// FakeIOSCall records one EnsureIOSAgent invocation FakeProvisioner
// received.
type FakeIOSCall struct {
	UDID string
	Kind IosAgentKind
}

// FakeProvisioner is a device-free AgentProvisioner test double: it
// records every call and returns a pre-configured error per key,
// so a sidecar test drives the ready and failed transitions without a
// real launcher, device, process, or network access.
type FakeProvisioner struct {
	mu            sync.Mutex
	iosResult     map[string]error
	androidResult map[string]error
	iosCalls      []FakeIOSCall
	androidCalls  []string
	iosKillCounts map[string]int

	// gate, when non-nil, is waited on before every call returns. A
	// test sets it to hold a call open through the "building" or
	// "installing" state so a dedup assertion observes that window
	// deterministically instead of racing a fake that would otherwise
	// resolve before the assertion runs.
	gate chan struct{}
}

// NewFakeProvisioner constructs a FakeProvisioner that succeeds every
// call immediately until configured otherwise.
func NewFakeProvisioner() *FakeProvisioner {
	return &FakeProvisioner{
		iosResult:     make(map[string]error),
		androidResult: make(map[string]error),
		iosKillCounts: make(map[string]int),
	}
}

var _ AgentProvisioner = (*FakeProvisioner)(nil)

// SetIOSResult makes the next EnsureIOSAgent calls for udid return
// err (nil for success).
func (f *FakeProvisioner) SetIOSResult(udid string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.iosResult[udid] = err
}

// SetAndroidResult makes the next EnsureAndroidAgent calls for serial
// return err (nil for success).
func (f *FakeProvisioner) SetAndroidResult(serial string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.androidResult[serial] = err
}

// SetGate installs gate: every in-flight and future call blocks until
// gate is closed (or its context is done). Passing nil clears it, so
// subsequent calls resolve immediately again.
func (f *FakeProvisioner) SetGate(gate chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = gate
}

// IOSCalls returns every EnsureIOSAgent call received so far.
func (f *FakeProvisioner) IOSCalls() []FakeIOSCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeIOSCall(nil), f.iosCalls...)
}

// AndroidCalls returns every EnsureAndroidAgent serial received so
// far.
func (f *FakeProvisioner) AndroidCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.androidCalls...)
}

// EnsureIOSAgent implements AgentProvisioner. The returned kill
// records its own invocation count against udid, retrievable via
// IOSKillCount, so a test can assert a teardown path actually
// stopped the simulated driver process instead of leaking it.
func (f *FakeProvisioner) EnsureIOSAgent(ctx context.Context, udid string, kind IosAgentKind) (func() error, error) {
	f.mu.Lock()
	f.iosCalls = append(f.iosCalls, FakeIOSCall{UDID: udid, Kind: kind})
	gate := f.gate
	err := f.iosResult[udid]
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return func() error { return nil }, ctx.Err()
		}
	}
	kill := func() error {
		f.mu.Lock()
		f.iosKillCounts[udid]++
		f.mu.Unlock()
		return nil
	}
	return kill, err
}

// IOSKillCount reports how many times the kill function returned by
// EnsureIOSAgent for udid has been invoked.
func (f *FakeProvisioner) IOSKillCount(udid string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.iosKillCounts[udid]
}

// EnsureAndroidAgent implements AgentProvisioner.
func (f *FakeProvisioner) EnsureAndroidAgent(ctx context.Context, serial string) error {
	f.mu.Lock()
	f.androidCalls = append(f.androidCalls, serial)
	gate := f.gate
	err := f.androidResult[serial]
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
