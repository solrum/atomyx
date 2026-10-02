package iosdriver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/iosdriver"
)

// signalOnSleepClock closes waiting the first time a poll loop sleeps,
// so a test knows a launch is blocked on a contended lock without
// timing anything, and then lets the loop spin until the lock frees.
type signalOnSleepClock struct {
	waiting chan struct{}
	once    sync.Once
}

func (c *signalOnSleepClock) NowMs() int64 { return 0 }

func (c *signalOnSleepClock) Sleep(ctx context.Context, _ int64) error {
	c.once.Do(func() { close(c.waiting) })
	runtime.Gosched()
	return ctx.Err()
}

var _ clock.Clock = (*signalOnSleepClock)(nil)

func newSimLauncherIn(t *testing.T, projectDir string, runner *fakeXcodebuildRunner, clk clock.Clock, cfg iosdriver.LaunchConfig) *iosdriver.XctestLauncher {
	t.Helper()
	cfg.ProjectDir = projectDir
	if cfg.Kind == "" {
		cfg.Kind = iosdriver.KindSimulator
	}
	return iosdriver.NewXctestLauncher(runner, runner, clk, cfg)
}

func TestXctestLauncher_EnsureRunning_ConcurrentLaunchesBuildOnceUnderTheBuildLock(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	var builds atomic.Int32
	building := make(chan struct{})
	releaseBuild := make(chan struct{})

	portA, portB := freeTCPPort(t), freeTCPPort(t)
	runnerA := &fakeXcodebuildRunner{spawnReady: true, reportData: `"simulatorUdid":"SIM-A"`, buildGate: func() {
		builds.Add(1)
		close(building)
		<-releaseBuild
	}}
	runnerB := &fakeXcodebuildRunner{spawnReady: true, reportData: `"simulatorUdid":"SIM-B"`, buildGate: func() { builds.Add(1) }}
	waiting := &signalOnSleepClock{waiting: make(chan struct{})}
	launcherA := newSimLauncherIn(t, projectDir, runnerA, &fakeClock{}, iosdriver.LaunchConfig{UDID: "SIM-A", Port: portA})
	launcherB := newSimLauncherIn(t, projectDir, runnerB, waiting, iosdriver.LaunchConfig{UDID: "SIM-B", Port: portB})

	type outcome struct {
		kill func() error
		err  error
	}
	resultA, resultB := make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		kill, err := launcherA.EnsureRunning(context.Background())
		resultA <- outcome{kill, err}
	}()
	<-building
	go func() {
		kill, err := launcherB.EnsureRunning(context.Background())
		resultB <- outcome{kill, err}
	}()
	<-waiting.waiting
	close(releaseBuild)

	a, b := <-resultA, <-resultB
	if a.err != nil || b.err != nil {
		t.Fatalf("EnsureRunning() errors = %v, %v, want both launches to succeed", a.err, b.err)
	}
	defer func() { _ = a.kill() }()
	defer func() { _ = b.kill() }()

	if got := builds.Load(); got != 1 {
		t.Fatalf("builds = %d, want exactly 1 because the second launch finds the first launch's product under the lock", got)
	}
	for _, inv := range runnerB.Invocations() {
		if inv.Args[0] == "build-for-testing" {
			t.Fatalf("second launch ran %+v, want it to skip the build", inv)
		}
	}
}

func TestXctestLauncher_EnsureRunning_BuildLockWaitHonorsCancellation(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	buildDir := filepath.Join(projectDir, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	holder := holdFlock(t, filepath.Join(buildDir, ".build.lock"))
	defer holder()

	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeXcodebuildRunner{spawnReady: true}
	l := newSimLauncherIn(t, projectDir, runner, &cancelOnSleepClock{cancel: cancel}, iosdriver.LaunchConfig{UDID: "SIM-1", Port: freeTCPPort(t)})

	_, err := l.EnsureRunning(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureRunning() error = %v, want context.Canceled while the lock is held elsewhere", err)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want nothing run while waiting on the lock", runner.Invocations())
	}
}

func TestXctestLauncher_EnsureRunning_EachTargetRunsFromItsOwnDerivedDataAndResultBundle(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())

	spawnArgsFor := func(udid string) []string {
		runner := &fakeXcodebuildRunner{spawnReady: true, reportData: `"simulatorUdid":"` + udid + `"`}
		l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{UDID: udid, Port: freeTCPPort(t)})
		kill, err := l.EnsureRunning(context.Background())
		if err != nil {
			t.Fatalf("EnsureRunning(%s) error = %v", udid, err)
		}
		defer func() { _ = kill() }()
		inv := runner.Invocations()
		if len(inv) != 1 {
			t.Fatalf("invocations for %s = %+v, want a single spawn and no build", udid, inv)
		}
		return inv[0].Args
	}
	argsA, argsB := spawnArgsFor("SIM-A"), spawnArgsFor("SIM-B")

	valueOf := func(args []string, flag string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		return ""
	}
	buildDir := filepath.Join(projectDir, "build")
	if got, want := valueOf(argsA, "-derivedDataPath"), filepath.Join(buildDir, "run-SIM-A"); got != want {
		t.Errorf("target A derived data = %s, want %s", got, want)
	}
	if got, want := valueOf(argsB, "-derivedDataPath"), filepath.Join(buildDir, "run-SIM-B"); got != want {
		t.Errorf("target B derived data = %s, want %s", got, want)
	}
	if valueOf(argsA, "-resultBundlePath") == valueOf(argsB, "-resultBundlePath") {
		t.Errorf("both targets write the result bundle %s", valueOf(argsA, "-resultBundlePath"))
	}
	if valueOf(argsA, "-xctestrun") != valueOf(argsB, "-xctestrun") {
		t.Errorf("targets run different test products: %s and %s", valueOf(argsA, "-xctestrun"), valueOf(argsB, "-xctestrun"))
	}
	if _, err := os.Stat(filepath.Join(buildDir, ".build.lock")); err != nil {
		t.Errorf("build lock file missing: %v", err)
	}
}

func TestXctestLauncher_EnsureRunning_ReportsBuildAndStartProgress(t *testing.T) {
	record := func(into *[]string) func(string) { return func(m string) { *into = append(*into, m) } }

	var cold []string
	projectDir, _ := newTestProjectDir(t)
	runner := &fakeXcodebuildRunner{spawnReady: true}
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{UDID: "SIM-1", Port: freeTCPPort(t), Progress: record(&cold)})
	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("cold EnsureRunning() error = %v", err)
	}
	_ = kill()
	if want := []string{"building runner", "starting runner"}; !equalArgs(cold, want) {
		t.Errorf("cold progress = %v, want %v", cold, want)
	}

	var warm []string
	warmDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, warmDir, iosdriver.KindSimulator, time.Now())
	l = newSimLauncherIn(t, warmDir, &fakeXcodebuildRunner{spawnReady: true}, &fakeClock{}, iosdriver.LaunchConfig{UDID: "SIM-1", Port: freeTCPPort(t), Progress: record(&warm)})
	kill, err = l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("warm EnsureRunning() error = %v", err)
	}
	_ = kill()
	if want := []string{"starting runner"}; !equalArgs(warm, want) {
		t.Errorf("warm progress = %v, want %v", warm, want)
	}
}

func TestXctestLauncher_EnsureRunning_AfterSpawnRunsBeforeTheReadinessWait(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindDevice, time.Now())
	gate := make(chan struct{})
	hostPort := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: hostPort, spawnReady: true, spawnGate: gate}
	var got iosdriver.SpawnedRunner
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{
		Kind: iosdriver.KindDevice, UDID: "DEV-1", DevTeam: "TEAM", RunnerID: "r-1",
		AllocatePort: func() (int, error) { return hostPort, nil },
		AfterSpawn: func(_ context.Context, spawned iosdriver.SpawnedRunner) (func(), error) {
			got = spawned
			close(gate)
			return func() {}, nil
		},
	})
	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v, want the runner to become reachable only once the hook ran", err)
	}
	defer func() { _ = kill() }()

	want := iosdriver.SpawnedRunner{HostPort: hostPort, RunnerPort: 22087, Pid: fakeRunnerPidBase + 1}
	if got != want {
		t.Errorf("AfterSpawn received %+v, want %+v", got, want)
	}
}

func TestXctestLauncher_EnsureRunning_AfterSpawnStopRunsAfterTheRunnerStops(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	spawnReturned := make(chan struct{})
	runner := &fakeXcodebuildRunner{spawnReady: true, spawnReturned: spawnReturned}
	var stops int
	var runnerGoneWhenHookStopped bool
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{
		UDID: "SIM-1", Port: freeTCPPort(t),
		AfterSpawn: func(context.Context, iosdriver.SpawnedRunner) (func(), error) {
			return func() {
				stops++
				select {
				case <-spawnReturned:
					runnerGoneWhenHookStopped = true
				default:
				}
			}, nil
		},
	})
	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	if stops != 0 {
		t.Fatalf("hook stop ran %d times before kill", stops)
	}
	_ = kill()
	_ = kill()
	if stops != 1 || !runnerGoneWhenHookStopped {
		t.Fatalf("hook stop ran %d times, runner gone first = %v, want exactly once and after the runner exited", stops, runnerGoneWhenHookStopped)
	}
}

func TestXctestLauncher_EnsureRunning_AfterSpawnFailureStopsTheRunner(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	spawnReturned := make(chan struct{})
	runner := &fakeXcodebuildRunner{spawnReady: false, spawnReturned: spawnReturned}
	hookErr := errors.New("tunnel refused")
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{
		UDID: "SIM-1", Port: freeTCPPort(t),
		AfterSpawn: func(context.Context, iosdriver.SpawnedRunner) (func(), error) { return nil, hookErr },
	})
	_, err := l.EnsureRunning(context.Background())
	if !errors.Is(err, hookErr) {
		t.Fatalf("EnsureRunning() error = %v, want the hook error", err)
	}
	select {
	case <-spawnReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("the spawned runner was still running after the hook failed")
	}
}

func TestXctestLauncher_EnsureRunning_ReadinessFailureRunsTheHookStop(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	runner := &fakeXcodebuildRunner{spawnReady: false}
	var stops int
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{
		UDID: "SIM-1", Port: freeTCPPort(t), LaunchTimeoutMs: 1_000,
		AfterSpawn: func(context.Context, iosdriver.SpawnedRunner) (func(), error) {
			return func() { stops++ }, nil
		},
	})
	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrLaunchTimeout)
	if stops != 1 {
		t.Fatalf("hook stop ran %d times, want 1 so a failed launch leaves no tunnel or state behind", stops)
	}
}

func TestXctestLauncher_EnsureRunning_TunnelBindConflictIsRetriedOnAFreshPort(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindDevice, time.Now())
	p1, p2 := freeTCPPort(t), freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: p2, spawnReady: true}
	var ports []int
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{
		Kind: iosdriver.KindDevice, UDID: "DEV-1", DevTeam: "TEAM", AllocatePort: sequencePorts(t, p1, p2),
		AfterSpawn: func(_ context.Context, spawned iosdriver.SpawnedRunner) (func(), error) {
			ports = append(ports, spawned.HostPort)
			if len(ports) == 1 {
				return nil, &iosdriver.IproxyError{Code: iosdriver.IproxyErrBindConflict, Message: "port taken"}
			}
			return func() {}, nil
		},
	})
	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v, want success on the second port", err)
	}
	defer func() { _ = kill() }()
	if len(ports) != 2 || ports[0] != p1 || ports[1] != p2 {
		t.Fatalf("hook ports = %v, want [%d %d]", ports, p1, p2)
	}
}

func TestXctestLauncher_EnsureRunning_RunnerPortOverridesTheDefault(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindDevice, time.Now())
	hostPort := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: hostPort, spawnReady: true}
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{
		Kind: iosdriver.KindDevice, UDID: "DEV-1", DevTeam: "TEAM", Port: hostPort, RunnerPort: 23456,
	})
	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	defer func() { _ = kill() }()
	if got := envValue(runner.SpawnEnvs()[0], "TEST_RUNNER_ATOMYX_PORT"); got != strconv.Itoa(23456) {
		t.Fatalf("TEST_RUNNER_ATOMYX_PORT = %q, want 23456", got)
	}
}

func TestXctestLauncher_EnsureRunning_BuildWithoutProductsIsABuildFailure(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	runner := &fakeXcodebuildRunner{spawnReady: true}
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{UDID: "SIM-1", Port: freeTCPPort(t), Scheme: "OtherScheme"})
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrBuildFailed)
	if !strings.Contains(err.Error(), "OtherScheme_iphonesimulator") {
		t.Fatalf("error = %v, want it to name the missing xctestrun file", err)
	}
}

func TestXctestLauncher_EnsureRunning_NewestXctestrunOfThePlatformWins(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	products := filepath.Join(projectDir, "build", "Build", "Products")
	old := filepath.Join(products, "AtomyxDriver_iphonesimulator18.2-arm64.xctestrun")
	device := filepath.Join(products, "AtomyxDriver_iphoneos27.0-arm64.xctestrun")
	for _, p := range []string{old, device} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(device, future, future); err != nil {
		t.Fatal(err)
	}
	runner := &fakeXcodebuildRunner{spawnReady: true}
	l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{UDID: "SIM-1", Port: freeTCPPort(t)})
	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	defer func() { _ = kill() }()
	args := runner.Invocations()[0].Args
	want := filepath.Join(products, "AtomyxDriver_iphonesimulator27.0-arm64.xctestrun")
	if !containsArgPair(args, "-xctestrun", want) {
		t.Fatalf("spawn args = %v, want the current simulator xctestrun %s, never a device or older one", args, want)
	}
}

func TestCheckRunnerIdentity_LegacyRunnerErrorNamesTheRebuildRemedy(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, ``)
	defer cleanup()

	_, _, err := iosdriver.CheckRunnerIdentity(context.Background(), port, time.Second,
		iosdriver.ExpectedRunner{UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err == nil || !err.Legacy {
		t.Fatalf("err = %+v, want a legacy runner identity error", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "rebuild the runner") || strings.Contains(msg, "choose another port") {
		t.Fatalf("message = %q, want the rebuild remedy and no advice to change the port", msg)
	}
}

func TestProvisionFailure_WrapsLauncherAndGenericErrorsAsAgentUnreachable(t *testing.T) {
	signing := &iosdriver.XctestLauncherError{Code: iosdriver.XctestErrSigning, Message: "devTeam is required. Set ATOMYX_DEV_TEAM."}
	code, message, _ := diag.Describe(iosdriver.ProvisionFailure("DEV-1", signing))
	if code != "AGENT_UNREACHABLE" || !strings.Contains(message, "ATOMYX_DEV_TEAM") {
		t.Errorf("signing failure = %s %q, want AGENT_UNREACHABLE carrying the signing remedy text", code, message)
	}

	code, message, remedy := diag.Describe(iosdriver.ProvisionFailure("DEV-1", errors.New("boom")))
	if code != "AGENT_UNREACHABLE" || !strings.Contains(message, "DEV-1") || remedy == "" {
		t.Errorf("generic failure = %s %q %q, want AGENT_UNREACHABLE naming the device with a remedy", code, message, remedy)
	}

	if err := iosdriver.ProvisionFailure("DEV-1", context.Canceled); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled = %v, want the context error unchanged", err)
	}
	if err := iosdriver.ProvisionFailure("DEV-1", nil); err != nil {
		t.Errorf("nil = %v, want nil", err)
	}
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(iosdriver.ProvisionFailure("DEV-1", signing), &launchErr) {
		t.Error("the launcher error is not reachable through errors.As")
	}
}

func TestXctestLauncher_EnsureRunning_ABuildMarksTheProductCurrentSoLaterLaunchesSkipIt(t *testing.T) {
	projectDir, pbxproj := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now().Add(-time.Hour))
	if err := os.Chtimes(pbxproj, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	launch := func(udid string) *fakeXcodebuildRunner {
		runner := &fakeXcodebuildRunner{spawnReady: true, reportData: `"simulatorUdid":"` + udid + `"`}
		l := newSimLauncherIn(t, projectDir, runner, &fakeClock{}, iosdriver.LaunchConfig{UDID: udid, Port: freeTCPPort(t)})
		kill, err := l.EnsureRunning(context.Background())
		if err != nil {
			t.Fatalf("EnsureRunning(%s) error = %v", udid, err)
		}
		_ = kill()
		return runner
	}
	builds := func(r *fakeXcodebuildRunner) int {
		n := 0
		for _, inv := range r.Invocations() {
			if inv.Args[0] == "build-for-testing" {
				n++
			}
		}
		return n
	}

	if got := builds(launch("SIM-A")); got != 1 {
		t.Fatalf("first launch builds = %d, want 1 for a stale product", got)
	}
	if got := builds(launch("SIM-B")); got != 0 {
		t.Fatalf("second launch builds = %d, want 0 because the first build stamped the product", got)
	}
}
