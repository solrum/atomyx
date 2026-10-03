package device

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/logging"
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
	// until it answers ready or a launcher failure is confirmed. port is
	// the host port the driver answers on, which a caller dials.
	//
	// kill is never nil: calling it stops the driver process this call
	// spawned, along with the USB tunnel it started for a physical
	// device and the runner record it wrote (a no-op if this call spawned
	// none, for example because a driver was already running). The caller
	// owns kill and must invoke it once the agent is no longer needed,
	// or the spawned process outlives the caller with nothing left able
	// to stop it.
	EnsureIOSAgent(ctx context.Context, udid string, kind IosAgentKind) (port int, kill func() error, err error)
	// EnsureIOSAgentWatched behaves like EnsureIOSAgent and also reports
	// the agent ending on its own. When the driver process, or the USB
	// tunnel of a physical device, exits after the agent answered ready
	// and before kill ran, the provisioner first ends whatever remains
	// through kill, then calls onLost once with a *ProcessExitError. A
	// call to kill is never reported. A nil onLost watches nothing.
	EnsureIOSAgentWatched(ctx context.Context, udid string, kind IosAgentKind, onLost func(error)) (port int, kill func() error, err error)
	EnsureAndroidAgent(ctx context.Context, serial string) error
	// ReinstallAndroidAgent replaces the agent on the Android device
	// serial and brings it up again, even when the installed agent
	// already answers its health probe. It fails with an
	// *androiddriver.AgentLauncherError whose code is ErrCodeNoAPK when
	// no agent APK can be found.
	ReinstallAndroidAgent(ctx context.Context, serial string) error
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
	// Inspector reads the start time of a launched runner and the host
	// boot time for the runner record. A nil Inspector defaults to
	// process.SystemInspector{}.
	Inspector process.Inspector
	// IosScheme, IosProjectFile, IosRunnerApp, and IosOnlyTesting
	// override the corresponding Xcode project identifiers the
	// XCUITest launcher builds and spawns against. Each defaults to
	// the standard Atomyx driver identifier when empty.
	IosScheme      string
	IosProjectFile string
	IosRunnerApp   string
	IosOnlyTesting string
	// Logger receives the "building runner" and "starting runner"
	// progress lines of a launch, so a first launch that compiles the
	// runner shows activity instead of hanging silently. A nil Logger
	// discards them.
	Logger logging.Logger
	// RunnerStateDir is the directory that holds the per-device runner
	// record. Empty disables the record. RunnerStateKey maps a device
	// UDID to the file key inside that directory. The directory and the
	// key come from the caller because this package does not know how
	// devices are keyed.
	RunnerStateDir string
	RunnerStateKey func(udid string) string
	// FixedIosPort makes every launch use the fixed control port 22087
	// instead of a port allocated per launch. A runner already answering
	// there is adopted, no runner record is written, and no tunnel is
	// started, because the driver that dials the runner starts its own.
	// A host that exposes a single fixed port to its own clients sets it.
	FixedIosPort bool
	// Keeper, when set, receives every runner this provisioner launches
	// so the process that owns the keeper can stop them when it exits.
	Keeper *RunnerKeeper
	// Reaper ends the runner an earlier holder of the device left
	// recorded, before a launch. The provisioner runs only under the
	// device lease, so a recorded runner belongs to a holder that is
	// gone or idle. Nil leaves a recorded runner alone, and so does
	// FixedIosPort, which writes no record.
	Reaper *RunnerReaper
}

// DriverProvisioner is the production AgentProvisioner: each call
// constructs a fresh XctestLauncher or AgentLauncher and blocks on its
// EnsureRunning. Apart from the set of host ports it has handed out, it
// holds no per-device state of its own. Dedup and status tracking belong
// to the caller.
type DriverProvisioner struct {
	runner     process.Runner
	background process.BackgroundRunner
	clock      orchestra.Clock
	logger     logging.Logger
	inspector  process.Inspector
	deps       DriverProvisionerDeps

	// freePort asks the operating system for a currently free host
	// port. It is a field so a test can script collisions.
	freePort func() (int, error)
	// newRunnerID returns a fresh launch identifier.
	newRunnerID func() string
	// now reports the current time for the runner record.
	now func() time.Time
	// ownPid reports the pid recorded as the owner of every runner this
	// provisioner launches or adopts.
	ownPid func() int
	// awaitWatchTick blocks until the next check of an adopted runner's
	// process is due, and fails once ctx ends.
	awaitWatchTick func(ctx context.Context) error

	mu       sync.Mutex
	reserved map[int]struct{}
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
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	inspector := deps.Inspector
	if inspector == nil {
		inspector = process.SystemInspector{}
	}
	p := &DriverProvisioner{
		runner: deps.Runner, background: background, clock: clock, logger: logger, inspector: inspector, deps: deps,
		freePort: operatingSystemFreePort, newRunnerID: randomRunnerID, now: time.Now, ownPid: os.Getpid,
		reserved: make(map[int]struct{}),
	}
	p.awaitWatchTick = p.defaultWatchTick
	return p
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

// reservePortAttempts bounds how many times reservePort asks the
// operating system for a port before giving up on finding one that no
// other launch in this process holds.
const reservePortAttempts = 16

// operatingSystemFreePort binds 127.0.0.1:0, reads the port the kernel
// chose, and releases it.
func operatingSystemFreePort() (int, error) {
	l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// randomRunnerID returns a random launch identifier, unique per launch.
func randomRunnerID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("device: read random bytes: %v", err))
	}
	return hex.EncodeToString(raw[:])
}

// reservePort returns a free host port that no other launch in this
// process holds. Binding 127.0.0.1:0 yields a port free at that moment,
// but the kernel can hand the same port out again once it is released,
// so the reserved set is what keeps two concurrent launches from
// receiving one port. A launch in another process can still take the
// port before the runner binds it, which the launcher's bounded retry
// covers.
func (p *DriverProvisioner) reservePort() (int, error) {
	for range reservePortAttempts {
		port, err := p.freePort()
		if err != nil {
			return 0, fmt.Errorf("reserve a host port: %w", err)
		}
		p.mu.Lock()
		_, taken := p.reserved[port]
		if !taken {
			p.reserved[port] = struct{}{}
		}
		p.mu.Unlock()
		if !taken {
			return port, nil
		}
	}
	return 0, fmt.Errorf("reserve a host port: every candidate was already held by another launch")
}

func (p *DriverProvisioner) releasePorts(ports ...int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, port := range ports {
		delete(p.reserved, port)
	}
}

// EnsureIOSAgent implements AgentProvisioner. Unless the provisioner
// was built with FixedIosPort, every call launches its own runner on a
// freshly reserved host port, tagged with a launch identifier the
// launcher requires the runner to echo, and for a physical device owns
// the USB tunnel that maps the host port onto the runner. The returned
// kill stops the runner, the tunnel and the runner record, and releases
// the reserved port. It runs its work once however many times it is
// called.
func (p *DriverProvisioner) EnsureIOSAgent(ctx context.Context, udid string, kind IosAgentKind) (int, func() error, error) {
	return p.EnsureIOSAgentWatched(ctx, udid, kind, nil)
}

// EnsureIOSAgentWatched implements AgentProvisioner.
func (p *DriverProvisioner) EnsureIOSAgentWatched(ctx context.Context, udid string, kind IosAgentKind, onLost func(error)) (int, func() error, error) {
	agent, err := p.ProvisionIOSAgent(ctx, udid, kind, onLost, ProvisionOptions{})
	return agent.Port, agent.Kill, err
}

// ProvisionIOSAgent implements AdoptingProvisioner. Unless the
// provisioner was built with FixedIosPort or opts.Fresh is set, it first
// adopts the simulator runner an earlier holder left running when every
// condition of adopt holds. Otherwise it ends the recorded runner and
// launches a new one.
func (p *DriverProvisioner) ProvisionIOSAgent(ctx context.Context, udid string, kind IosAgentKind, onLost func(error), opts ProvisionOptions) (IOSAgent, error) {
	// A process can exit right after the launch settles, before this call
	// has built its kill function, so a loss waits on settled for it. A
	// launch that failed leaves ended nil, and its caller already has
	// the failure, so nothing is reported.
	var ended func() error
	var held launchRecord
	settled := make(chan struct{})
	defer close(settled)
	lostHook := func(component LostComponent) func(error) {
		if onLost == nil {
			return nil
		}
		return func(verdict error) {
			<-settled
			if ended == nil {
				return
			}
			if held.ownedByAnotherProcess() {
				p.logger.Debug("runner ended while another process owns it", logging.LogContext{"udid": udid})
				return
			}
			_ = ended()
			onLost(&ProcessExitError{Component: component, Cause: verdict})
		}
	}

	cfg := p.iosLaunchConfig(udid, kind)
	cfg.OnRunnerExit = lostHook(LostRunner)
	if !p.deps.FixedIosPort {
		if !opts.Fresh {
			if state, ok := p.adopt(ctx, udid, cfg); ok {
				held = launchRecord{udid: udid, path: p.statePath(udid), runnerID: state.RunnerID, owner: RunnerOwner{Pid: state.OwnerPid, StartTime: state.OwnerStartTime}}
				return p.adoptedAgent(ctx, state, held, udid, lostHook(LostRunner), func(stop func() error) { ended = stop }), nil
			}
		}
		p.reapOrphan(ctx, udid)
		cfg.RunnerID = p.newRunnerID()
		cfg.AllocatePort = p.reservePort
		cfg.ReleasePort = func(port int) { p.releasePorts(port) }
		cfg.AfterSpawn = p.afterSpawn(udid, kind, cfg.RunnerID, lostHook(LostTunnel), &held)
	}

	launcher := iosdriver.NewXctestLauncher(p.runner, p.background, p.clock, cfg)
	kill, err := launcher.EnsureRunning(ctx)
	if err != nil {
		return IOSAgent{Kill: kill}, err
	}
	if p.deps.FixedIosPort {
		ended = kill
		return IOSAgent{Port: launcher.Port(), Kill: kill}, nil
	}
	port := launcher.Port()
	stop := sync.OnceValue(func() error {
		err := kill()
		p.releasePorts(port)
		if p.deps.Keeper != nil {
			p.deps.Keeper.forget(cfg.RunnerID)
		}
		return err
	})
	ended = stop
	if p.deps.Keeper != nil {
		p.deps.Keeper.keep(held, stop)
	}
	return IOSAgent{Port: port, RunnerID: cfg.RunnerID, Kill: stop}, nil
}

// statePath returns the runner record path for udid, or an empty string
// when records are disabled.
func (p *DriverProvisioner) statePath(udid string) string {
	if p.deps.RunnerStateDir == "" || p.deps.RunnerStateKey == nil {
		return ""
	}
	return RunnerStatePath(p.deps.RunnerStateDir, p.deps.RunnerStateKey(udid))
}

// reapOrphan ends the runner an earlier holder of udid left recorded. A
// failure to end it is logged and does not stop the launch, because the
// launcher still refuses a runner that is not its own.
func (p *DriverProvisioner) reapOrphan(ctx context.Context, udid string) {
	path := p.statePath(udid)
	if p.deps.Reaper == nil || path == "" {
		return
	}
	outcome, err := p.deps.Reaper.Reap(ctx, path, udid)
	fields := logging.LogContext{"udid": udid, "pgid": outcome.PGID}
	switch {
	case err != nil:
		fields["error"] = err.Error()
		p.logger.Warn("could not end an orphaned runner", fields)
	case outcome.Signaled || outcome.TerminatedViaSimctl:
		p.logger.Info("ended an orphaned runner", fields)
	}
	if outcome.TunnelSignaled {
		p.logger.Info("ended an orphaned iproxy tunnel", logging.LogContext{"udid": udid})
	}
}

// Prepare implements RunnerPreparer. It builds the iOS runner when it is
// missing or stale, under the shared build lock, and starts nothing. An
// Android device, a descriptor with no id, and a descriptor that carries
// a port, which means its runner is already served, need no build and
// return nil at once.
func (p *DriverProvisioner) Prepare(ctx context.Context, desc Descriptor) error {
	if desc.Platform != PlatformIOS || desc.ID == "" || desc.Port != 0 {
		return nil
	}
	kind := IosAgentKindDevice
	if desc.Kind == KindSimulator {
		kind = IosAgentKindSimulator
	}
	err := iosdriver.NewXctestLauncher(p.runner, p.background, p.clock, p.iosLaunchConfig(desc.ID, kind)).EnsureBuilt(ctx)
	return iosdriver.ProvisionFailure(desc.ID, err)
}

var _ RunnerPreparer = (*DriverProvisioner)(nil)

// iosLaunchConfig is the launch configuration shared by building and
// launching the runner for udid, so both phases agree on the project,
// the signing identity and the build products they look at.
func (p *DriverProvisioner) iosLaunchConfig(udid string, kind IosAgentKind) iosdriver.LaunchConfig {
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
	progress := p.logger.Child(logging.LogContext{"udid": udid, "kind": string(kind)})
	cfg := iosdriver.LaunchConfig{
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
		Progress:                     func(message string) { progress.Info(message, nil) },
	}
	return cfg
}

// afterSpawn builds the launcher hook that runs once the runner process
// exists and before it is probed: it records the runner for crash
// recovery, then for a physical device starts the USB tunnel the probe
// needs. The stop function it returns removes both.
func (p *DriverProvisioner) afterSpawn(udid string, kind IosAgentKind, runnerID string, onTunnelExit func(error), held *launchRecord) func(context.Context, iosdriver.SpawnedRunner) (func(), error) {
	return func(ctx context.Context, spawned iosdriver.SpawnedRunner) (func(), error) {
		statePath, state, err := p.recordRunner(udid, kind, runnerID, spawned)
		if err != nil {
			return nil, err
		}
		*held = launchRecord{udid: udid, path: statePath, runnerID: runnerID, owner: RunnerOwner{Pid: state.OwnerPid, StartTime: state.OwnerStartTime}}
		removeRecord := func() {
			if statePath != "" {
				removeRunnerStateOf(statePath, runnerID, held.owner)
			}
		}
		if kind != IosAgentKindDevice {
			return removeRecord, nil
		}
		stopTunnel, tunnelPid, err := iosdriver.StartTunnel(ctx, p.background, p.clock, iosdriver.TunnelConfig{
			UDID: udid, HostPort: spawned.HostPort, RunnerID: runnerID, OnExit: onTunnelExit,
		})
		if err != nil {
			removeRecord()
			return nil, err
		}
		if tunnelPid > 0 && statePath != "" {
			if err := p.recordTunnel(statePath, state, tunnelPid); err != nil {
				stopTunnel()
				removeRecord()
				return nil, err
			}
		}
		return func() {
			if stopTunnel != nil {
				stopTunnel()
			}
			removeRecord()
		}, nil
	}
}

// recordRunner writes the runner record for a spawned runner and
// returns its path and content, or an empty path when records are
// disabled.
func (p *DriverProvisioner) recordRunner(udid string, kind IosAgentKind, runnerID string, spawned iosdriver.SpawnedRunner) (string, RunnerState, error) {
	path := p.statePath(udid)
	if path == "" {
		return "", RunnerState{}, nil
	}
	info, err := p.inspector.Inspect(spawned.Pid)
	if err != nil {
		return "", RunnerState{}, fmt.Errorf("record the runner: %w", err)
	}
	booted, err := p.inspector.BootTime()
	if err != nil {
		return "", RunnerState{}, fmt.Errorf("record the runner: %w", err)
	}
	ownerPid := p.ownPid()
	ownerInfo, err := p.inspector.Inspect(ownerPid)
	if err != nil {
		return "", RunnerState{}, fmt.Errorf("record the runner: %w", err)
	}
	state := RunnerState{
		UDID:             udid,
		Kind:             string(kind),
		RunnerID:         runnerID,
		HostPort:         spawned.HostPort,
		Pid:              spawned.Pid,
		Pgid:             spawned.Pid,
		ProcessStartTime: info.StartTime,
		HostBootTime:     booted,
		StartedAt:        p.now().UTC(),
		OwnerPid:         ownerPid,
		OwnerStartTime:   ownerInfo.StartTime,

		BuildProductPath:    spawned.Product.Path,
		BuildProductModTime: spawned.Product.ModTime,
	}
	if err := WriteRunnerState(path, state); err != nil {
		return "", RunnerState{}, err
	}
	return path, state, nil
}

// recordTunnel rewrites the runner record at path with the iproxy process
// that tunnels the runner's port, so a later process can end it when this
// one dies without stopping it.
func (p *DriverProvisioner) recordTunnel(path string, state RunnerState, tunnelPid int) error {
	info, err := p.inspector.Inspect(tunnelPid)
	if err != nil {
		return fmt.Errorf("record the tunnel: %w", err)
	}
	state.TunnelPid = tunnelPid
	state.TunnelStartTime = info.StartTime
	return WriteRunnerState(path, state)
}

// EnsureAndroidAgent implements AgentProvisioner. The host port is the
// forward adb already holds for serial, or a fresh one adb allocates,
// so two devices never contend for one fixed host port.
func (p *DriverProvisioner) EnsureAndroidAgent(ctx context.Context, serial string) error {
	launcher, err := p.androidLauncher(ctx, serial)
	if err != nil {
		return err
	}
	return launcher.EnsureRunning(ctx)
}

// ReinstallAndroidAgent implements AgentProvisioner.
func (p *DriverProvisioner) ReinstallAndroidAgent(ctx context.Context, serial string) error {
	launcher, err := p.androidLauncher(ctx, serial)
	if err != nil {
		return err
	}
	return launcher.Reinstall(ctx)
}

func (p *DriverProvisioner) androidLauncher(ctx context.Context, serial string) (*androiddriver.AgentLauncher, error) {
	apkPath := p.deps.AndroidAPKPath
	if apkPath == "" {
		apkPath = os.Getenv("ATOMYX_ANDROID_APK")
	}
	hostPort, err := androiddriver.ForwardHostPort(ctx, p.runner, serial, androidControlPort)
	if err != nil {
		return nil, err
	}
	client := transport.NewHTTPClient("127.0.0.1", hostPort)
	return androiddriver.NewAgentLauncher(p.runner, p.clock, client, androiddriver.AgentLauncherOptions{
		Serial:     serial,
		HostPort:   hostPort,
		DevicePort: androidControlPort,
		APKPath:    apkPath,
	}), nil
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
	mu              sync.Mutex
	iosResult       map[string]error
	androidResult   map[string]error
	reinstallResult map[string]error
	reinstallCalls  []string
	iosCalls        []FakeIOSCall
	androidCalls    []string
	iosKillCounts   map[string]int
	iosLost         map[string]func(error)
	iosKills        map[string]func() error
	iosPorts        map[string]int
	iosAdopted      map[string]bool
	iosOptions      []ProvisionOptions
	keeper          *RunnerKeeper

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
		iosResult:       make(map[string]error),
		androidResult:   make(map[string]error),
		reinstallResult: make(map[string]error),
		iosKillCounts:   make(map[string]int),
		iosLost:         make(map[string]func(error)),
		iosKills:        make(map[string]func() error),
		iosPorts:        make(map[string]int),
		iosAdopted:      make(map[string]bool),
	}
}

var (
	_ AgentProvisioner    = (*FakeProvisioner)(nil)
	_ AdoptingProvisioner = (*FakeProvisioner)(nil)
)

// SetIOSAdopted makes every later ProvisionIOSAgent call for udid report
// an adopted runner, unless the call asks for a fresh one.
func (f *FakeProvisioner) SetIOSAdopted(udid string, adopted bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.iosAdopted[udid] = adopted
}

// IOSOptions returns the ProvisionOptions of every call received so far.
func (f *FakeProvisioner) IOSOptions() []ProvisionOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ProvisionOptions(nil), f.iosOptions...)
}

// KeepWith hands the runner of every later EnsureIOSAgent call to keeper,
// the way the real provisioner does, so a test sees the keeper stop it.
func (f *FakeProvisioner) KeepWith(keeper *RunnerKeeper) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keeper = keeper
}

// SetIOSResult makes the next EnsureIOSAgent calls for udid return
// err (nil for success).
func (f *FakeProvisioner) SetIOSResult(udid string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.iosResult[udid] = err
}

// SetIOSPort makes EnsureIOSAgent report port for udid. The default is
// the fixed control port.
func (f *FakeProvisioner) SetIOSPort(udid string, port int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.iosPorts[udid] = port
}

// SetAndroidResult makes the next EnsureAndroidAgent calls for serial
// return err (nil for success).
func (f *FakeProvisioner) SetAndroidResult(serial string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.androidResult[serial] = err
}

// SetReinstallResult makes the next ReinstallAndroidAgent calls for
// serial return err (nil for success).
func (f *FakeProvisioner) SetReinstallResult(serial string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reinstallResult[serial] = err
}

// ReinstallCalls returns every ReinstallAndroidAgent serial received
// so far.
func (f *FakeProvisioner) ReinstallCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reinstallCalls...)
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

// EnsureIOSAgent implements AgentProvisioner. It watches nothing. The
// returned kill records its own invocation count against udid, retrievable via
// IOSKillCount, so a test can assert a teardown path actually
// stopped the simulated driver process instead of leaking it.
func (f *FakeProvisioner) EnsureIOSAgent(ctx context.Context, udid string, kind IosAgentKind) (int, func() error, error) {
	return f.EnsureIOSAgentWatched(ctx, udid, kind, nil)
}

// EnsureIOSAgentWatched implements AgentProvisioner. FireLost reports a
// loss through the onLost of the latest call for the same udid.
func (f *FakeProvisioner) EnsureIOSAgentWatched(ctx context.Context, udid string, kind IosAgentKind, onLost func(error)) (int, func() error, error) {
	agent, err := f.ProvisionIOSAgent(ctx, udid, kind, onLost, ProvisionOptions{})
	return agent.Port, agent.Kill, err
}

// ProvisionIOSAgent implements AdoptingProvisioner. The runner is
// reported adopted when SetIOSAdopted asked for it and opts.Fresh is
// not set.
func (f *FakeProvisioner) ProvisionIOSAgent(ctx context.Context, udid string, kind IosAgentKind, onLost func(error), opts ProvisionOptions) (IOSAgent, error) {
	f.mu.Lock()
	f.iosLost[udid] = onLost
	f.mu.Unlock()
	f.mu.Lock()
	f.iosCalls = append(f.iosCalls, FakeIOSCall{UDID: udid, Kind: kind})
	f.iosOptions = append(f.iosOptions, opts)
	adopted := f.iosAdopted[udid] && !opts.Fresh
	gate := f.gate
	err := f.iosResult[udid]
	port, ok := f.iosPorts[udid]
	if !ok {
		port = iosControlPort
	}
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return IOSAgent{Kill: func() error { return nil }}, ctx.Err()
		}
	}
	var once sync.Once
	kill := func() error {
		once.Do(func() {
			f.mu.Lock()
			f.iosKillCounts[udid]++
			f.mu.Unlock()
		})
		return nil
	}
	f.mu.Lock()
	f.iosKills[udid] = kill
	keeper := f.keeper
	f.mu.Unlock()
	if keeper != nil && err == nil {
		keeper.keep(launchRecord{udid: udid, runnerID: udid}, kill)
	}
	return IOSAgent{Port: port, RunnerID: "fake-runner-" + udid, Adopted: adopted && err == nil, Kill: kill}, err
}

// FireLost simulates the provisioned runner of udid exiting on its own.
// Like the real provisioner it ends the agent through kill first, then
// reports a *ProcessExitError wrapping cause through the onLost of the
// latest EnsureIOSAgentWatched call. It does nothing when no call
// installed an onLost.
func (f *FakeProvisioner) FireLost(udid string, cause error) {
	f.mu.Lock()
	onLost, kill := f.iosLost[udid], f.iosKills[udid]
	f.mu.Unlock()
	if onLost == nil {
		return
	}
	_ = kill()
	onLost(&ProcessExitError{Component: LostRunner, Cause: cause})
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

// ReinstallAndroidAgent implements AgentProvisioner.
func (f *FakeProvisioner) ReinstallAndroidAgent(_ context.Context, serial string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reinstallCalls = append(f.reinstallCalls, serial)
	return f.reinstallResult[serial]
}

// launchRecord names the runner record one launch wrote, so the loss
// hooks can tell whether the record still belongs to this launch.
type launchRecord struct {
	udid     string
	path     string
	runnerID string
	owner    RunnerOwner
}

// ownedByAnotherProcess reports whether the record of this launch now
// names a different owner for the same runner. Such a runner serves
// another process, so this launch must neither stop its tunnel nor remove
// its record, and has no session left to tell.
func (r launchRecord) ownedByAnotherProcess() bool {
	if r.path == "" {
		return false
	}
	state, err := ReadRunnerState(r.path)
	return err == nil && state.RunnerID == r.runnerID && !state.OwnedBy(r.owner)
}
