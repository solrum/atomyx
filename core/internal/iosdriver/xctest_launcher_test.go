package iosdriver_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

// fakeClock is a deterministic clock.Clock test double. Sleep
// advances the clock's current time immediately instead of blocking
// on a real timer, so a test exercises XctestLauncher's poll and
// timeout logic without waiting on real time.
type fakeClock struct {
	current int64
}

func (c *fakeClock) NowMs() int64 { return c.current }

func (c *fakeClock) Sleep(ctx context.Context, ms int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ms > 0 {
		c.current += ms
	}
	return nil
}

var _ clock.Clock = (*fakeClock)(nil)

// fakeXcodebuildRunner is the process.Runner and
// process.BackgroundRunner test double for XctestLauncher. It records
// every invocation, from either entry point, in dispatch order for
// argv assertions. Run answers "build-for-testing" with a canned
// Result and error, and refuses "test-without-building" (recording
// it in spawnViaRun) because that long-lived process must go through
// Start. For "test-without-building", Start either returns
// spawnStartErr (modeling a missing binary), starts a process that
// opens a real listener on the configured port that answers the same
// ping handshake startFakeTunnelListener uses (modeling a driver that
// came up), exits immediately with spawnExitStderr (modeling an early
// crash), or blocks without ever listening (modeling a driver that
// never becomes reachable) until the launcher kills it.
type fakeXcodebuildRunner struct {
	mu          sync.Mutex
	invocations []process.Invocation
	spawnViaRun bool

	buildResult process.Result
	buildErr    error

	port            int
	spawnReady      bool
	spawnStartErr   error
	spawnExit       bool
	spawnExitStderr string

	// spawnChatterBytes is how much the process writes to its stdout
	// before doing anything else. The pipe is unbuffered, so the write
	// blocks until the launcher drains it, modeling a verbose xcodebuild.
	spawnChatterBytes int

	// spawnReturned, when non-nil, is closed just before the spawned
	// process body returns on the spawnReady path, so a test can
	// observe that Kill actually made the simulated xcodebuild
	// process exit instead of only asserting that the caller's kill
	// call returned.
	spawnReturned chan struct{}
}

func (r *fakeXcodebuildRunner) record(name string, args []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invocations = append(r.invocations, process.Invocation{Name: name, Args: append([]string(nil), args...)})
}

func (r *fakeXcodebuildRunner) Run(_ context.Context, name string, args ...string) (process.Result, error) {
	r.record(name, args)
	if len(args) > 0 && args[0] == "test-without-building" {
		r.mu.Lock()
		r.spawnViaRun = true
		r.mu.Unlock()
		return process.Result{}, errors.New("test-without-building must be started through the BackgroundRunner")
	}
	if len(args) > 0 && args[0] == "build-for-testing" {
		return r.buildResult, r.buildErr
	}
	return process.Result{}, nil
}

func (r *fakeXcodebuildRunner) Start(ctx context.Context, name string, args ...string) (process.BackgroundProcess, error) {
	r.record(name, args)
	if r.spawnStartErr != nil {
		return nil, r.spawnStartErr
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	pr, pw := io.Pipe()
	proc := &tunnelProcess{cancel: cancel, stdout: pr, done: make(chan struct{})}
	go func() {
		defer close(proc.done)
		defer func() { _ = pw.Close() }()
		proc.err = r.runSpawn(ctx, proc, pw)
	}()
	return proc, nil
}

func (r *fakeXcodebuildRunner) runSpawn(ctx context.Context, proc *tunnelProcess, stdout io.Writer) error {
	if r.spawnChatterBytes > 0 {
		_, _ = stdout.Write(make([]byte, r.spawnChatterBytes))
	}
	if r.spawnExit {
		proc.exitCode = 65
		proc.stderr = r.spawnExitStderr
		return errors.New("exit status 65")
	}
	if !r.spawnReady {
		<-ctx.Done()
		if r.spawnReturned != nil {
			close(r.spawnReturned)
		}
		return ctx.Err()
	}

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", r.port))
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go answerPingHandshake(c)
		}
	}()

	<-ctx.Done()
	_ = listener.Close()
	<-done
	if r.spawnReturned != nil {
		close(r.spawnReturned)
	}
	return ctx.Err()
}

func (r *fakeXcodebuildRunner) Invocations() []process.Invocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]process.Invocation(nil), r.invocations...)
}

func (r *fakeXcodebuildRunner) SpawnedViaRun() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spawnViaRun
}

var (
	_ process.Runner           = (*fakeXcodebuildRunner)(nil)
	_ process.BackgroundRunner = (*fakeXcodebuildRunner)(nil)
)

// answerPingHandshake answers the same ping handshake
// probeIproxyPing sends, on a single already-accepted connection.
func answerPingHandshake(c net.Conn) {
	defer func() { _ = c.Close() }()
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return
	}
	_ = buf[:n]
	_, _ = c.Write([]byte(`{"id":0,"ok":true,"data":{"pong":true}}` + "\n"))
}

// newTestProjectDir creates a temp directory containing
// AtomyxDriver.xcodeproj/project.pbxproj, matching the layout
// EnsureRunning requires before it will build or spawn anything. It
// returns the project directory and the pbxproj file's path.
func newTestProjectDir(t *testing.T) (dir, pbxproj string) {
	t.Helper()
	dir = t.TempDir()
	xcodeproj := filepath.Join(dir, "AtomyxDriver.xcodeproj")
	if err := os.MkdirAll(xcodeproj, 0o755); err != nil {
		t.Fatalf("mkdir xcodeproj: %v", err)
	}
	pbxproj = filepath.Join(xcodeproj, "project.pbxproj")
	if err := os.WriteFile(pbxproj, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write pbxproj: %v", err)
	}
	return dir, pbxproj
}

// createFreshRunnerApp creates the compiled test-runner product path
// EnsureRunning stats for staleness, with its modification time set
// to mtime, and returns its path.
func createFreshRunnerApp(t *testing.T, projectDir string, kind iosdriver.Kind, mtime time.Time) string {
	t.Helper()
	config := "Debug-iphonesimulator"
	if kind == iosdriver.KindDevice {
		config = "Debug-iphoneos"
	}
	runnerApp := filepath.Join(projectDir, "build", "Build", "Products", config, "AtomyxDriverAgent-Runner.app")
	if err := os.MkdirAll(runnerApp, 0o755); err != nil {
		t.Fatalf("mkdir runnerApp: %v", err)
	}
	if err := os.Chtimes(runnerApp, mtime, mtime); err != nil {
		t.Fatalf("chtimes runnerApp: %v", err)
	}
	return runnerApp
}

func wantBuildArgs(destination string, signOverrides ...string) []string {
	args := []string{
		"build-for-testing",
		"-project", "PROJECT",
		"-scheme", "AtomyxDriver",
		"-destination", destination,
		"-derivedDataPath", "DERIVED",
	}
	return append(args, signOverrides...)
}

func wantSpawnArgs(destination string, signOverrides ...string) []string {
	args := []string{
		"test-without-building",
		"-project", "PROJECT",
		"-scheme", "AtomyxDriver",
		"-destination", destination,
		"-derivedDataPath", "DERIVED",
		"-only-testing:AtomyxDriverAgent/AtomyxDriverAgent/testServeCommands",
	}
	return append(args, signOverrides...)
}

var simulatorSignOverrides = []string{
	"CODE_SIGN_STYLE=Automatic",
	"CODE_SIGNING_REQUIRED=NO",
	"CODE_SIGNING_ALLOWED=NO",
	"CODE_SIGN_IDENTITY=",
	"PROVISIONING_PROFILE_SPECIFIER=",
	"DEVELOPMENT_TEAM=",
}

func deviceSignOverrides(team string) []string {
	return []string{
		"CODE_SIGN_STYLE=Automatic",
		"CODE_SIGNING_REQUIRED=YES",
		"CODE_SIGNING_ALLOWED=YES",
		"DEVELOPMENT_TEAM=" + team,
	}
}

// substituteProjectPaths rewrites the placeholder "PROJECT" and
// "DERIVED" tokens produced by wantBuildArgs / wantSpawnArgs into the
// absolute paths EnsureRunning actually constructs from projectDir,
// so a test can express expectations independently of the temp
// directory a given run allocates.
func substituteProjectPaths(args []string, projectDir string) []string {
	xcodeproj := filepath.Join(projectDir, "AtomyxDriver.xcodeproj")
	derived := filepath.Join(projectDir, "build")
	out := make([]string, len(args))
	for i, a := range args {
		switch a {
		case "PROJECT":
			out[i] = xcodeproj
		case "DERIVED":
			out[i] = derived
		default:
			out[i] = a
		}
	}
	return out
}

func TestXctestLauncher_EnsureRunning_ProbeAnswers_SkipsBuildAndSpawn(t *testing.T) {
	port, cleanup := startFakeTunnelListener(t, true)
	defer cleanup()

	runner := &fakeXcodebuildRunner{}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: t.TempDir(),
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want none when the driver already answers the ping handshake", runner.Invocations())
	}
	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v, want a no-op success since this call spawned nothing", err)
	}
}

func TestXctestLauncher_EnsureRunning_NoProjectDirectory_ReturnsNoProjectError(t *testing.T) {
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: t.TempDir(),
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	_, err := l.EnsureRunning(context.Background())
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) {
		t.Fatalf("EnsureRunning() error = %v (%T), want *iosdriver.XctestLauncherError", err, err)
	}
	if launchErr.Code != iosdriver.XctestErrNoProject {
		t.Fatalf("Code = %q, want %q", launchErr.Code, iosdriver.XctestErrNoProject)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want none when the xcodeproj is missing", runner.Invocations())
	}
}

func TestXctestLauncher_EnsureRunning_BuildsThenSpawnsWithExactArgv_Simulator(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	if runner.SpawnedViaRun() {
		t.Fatal("test-without-building went through Runner.Run, want the BackgroundRunner so the fallback timeout cannot kill it")
	}
	invocations := runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("invocations = %+v, want exactly one build then one spawn", invocations)
	}

	wantBuild := substituteProjectPaths(
		wantBuildArgs("platform=iOS Simulator,id=SIM-1", simulatorSignOverrides...),
		projectDir,
	)
	if invocations[0].Name != "xcodebuild" || !equalArgs(invocations[0].Args, wantBuild) {
		t.Fatalf("build invocation = %+v, want xcodebuild %v", invocations[0], wantBuild)
	}

	wantSpawn := substituteProjectPaths(
		wantSpawnArgs("platform=iOS Simulator,id=SIM-1", simulatorSignOverrides...),
		projectDir,
	)
	if invocations[1].Name != "xcodebuild" || !equalArgs(invocations[1].Args, wantSpawn) {
		t.Fatalf("spawn invocation = %+v, want xcodebuild %v", invocations[1], wantSpawn)
	}
}

// TestXctestLauncher_EnsureRunning_KillStopsTheSpawnedXcodebuildProcess
// guards the orphaned-process regression: EnsureRunning's returned
// kill must actually reach the spawned "xcodebuild test-without-
// building" invocation, not just retain a CancelFunc nobody calls.
func TestXctestLauncher_EnsureRunning_KillStopsTheSpawnedXcodebuildProcess(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	port := freeTCPPort(t)
	spawnReturned := make(chan struct{})
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true, spawnReturned: spawnReturned}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	select {
	case <-spawnReturned:
		t.Fatal("the spawned xcodebuild invocation returned before kill was called")
	default:
	}

	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	select {
	case <-spawnReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("kill() returned but the spawned xcodebuild invocation never exited")
	}

	// A second call must not hang: sync.Once guards the drain of the
	// single-send exited channel spawnAndWait closes over.
	if err := kill(); err != nil {
		t.Fatalf("second kill() error = %v", err)
	}
}

func TestXctestLauncher_EnsureRunning_VerboseStdoutDoesNotStallReadiness(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true, spawnChatterBytes: 4 << 20}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v, want readiness despite 4 MiB of stdout", err)
	}
	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
}

func TestXctestLauncher_EnsureRunning_EmptyStderr_ReturnsSpawnExitError(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnExit: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	_, err := l.EnsureRunning(context.Background())
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iosdriver.XctestErrSpawnExit {
		t.Fatalf("EnsureRunning() error = %v, want an XctestErrSpawnExit launcher error", err)
	}
	if !strings.Contains(launchErr.Message, "stderr: (empty)") {
		t.Fatalf("Message = %q, want the empty-stderr marker", launchErr.Message)
	}
}

func TestXctestLauncher_EnsureRunning_PhysicalDevice_UsesDeviceDestinationAndSigningArgv(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindDevice,
		UDID:       "DEV-1",
		DevTeam:    "TEAM123",
	})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("invocations = %+v, want exactly one build then one spawn", invocations)
	}

	wantBuild := substituteProjectPaths(
		wantBuildArgs("platform=iOS,id=DEV-1", deviceSignOverrides("TEAM123")...),
		projectDir,
	)
	if !equalArgs(invocations[0].Args, wantBuild) {
		t.Fatalf("build args = %+v, want %v", invocations[0].Args, wantBuild)
	}

	wantSpawn := substituteProjectPaths(
		wantSpawnArgs("platform=iOS,id=DEV-1", deviceSignOverrides("TEAM123")...),
		projectDir,
	)
	if !equalArgs(invocations[1].Args, wantSpawn) {
		t.Fatalf("spawn args = %+v, want %v", invocations[1].Args, wantSpawn)
	}
}

func TestXctestLauncher_EnsureRunning_PhysicalDeviceWithoutDevTeam_ReturnsSigningError(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindDevice,
		UDID:       "DEV-1",
	})

	_, err := l.EnsureRunning(context.Background())
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) {
		t.Fatalf("EnsureRunning() error = %v (%T), want *iosdriver.XctestLauncherError", err, err)
	}
	if launchErr.Code != iosdriver.XctestErrSigning {
		t.Fatalf("Code = %q, want %q", launchErr.Code, iosdriver.XctestErrSigning)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want no xcodebuild call without a devTeam", runner.Invocations())
	}
}

func TestXctestLauncher_EnsureRunning_SkipsBuildWhenRunnerAppIsFresh(t *testing.T) {
	projectDir, pbxproj := newTestProjectDir(t)
	if err := os.Chtimes(pbxproj, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes pbxproj: %v", err)
	}
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())

	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) != 1 || !equalArgs(invocations[0].Args[:1], []string{"test-without-building"}) {
		t.Fatalf("invocations = %+v, want exactly one spawn and no build for a fresh runner app", invocations)
	}
}

func TestXctestLauncher_EnsureRunning_RebuildsWhenPbxprojNewerThanRunnerApp(t *testing.T) {
	projectDir, pbxproj := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now().Add(-time.Hour))
	if err := os.Chtimes(pbxproj, time.Now(), time.Now()); err != nil {
		t.Fatalf("chtimes pbxproj: %v", err)
	}

	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) != 2 || invocations[0].Args[0] != "build-for-testing" {
		t.Fatalf("invocations = %+v, want a build before the spawn when project.pbxproj is newer than the runner app", invocations)
	}
}

func TestXctestLauncher_EnsureRunning_RebuildsWhenSwiftSourceNewerThanRunnerApp(t *testing.T) {
	projectDir, pbxproj := newTestProjectDir(t)
	if err := os.Chtimes(pbxproj, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes pbxproj: %v", err)
	}
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now().Add(-time.Hour))

	sourcesDir := filepath.Join(projectDir, "Sources")
	if err := os.MkdirAll(sourcesDir, 0o755); err != nil {
		t.Fatalf("mkdir Sources: %v", err)
	}
	swiftFile := filepath.Join(sourcesDir, "Driver.swift")
	if err := os.WriteFile(swiftFile, []byte("// swift"), 0o644); err != nil {
		t.Fatalf("write swift file: %v", err)
	}
	if err := os.Chtimes(swiftFile, time.Now(), time.Now()); err != nil {
		t.Fatalf("chtimes swift file: %v", err)
	}

	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) != 2 || invocations[0].Args[0] != "build-for-testing" {
		t.Fatalf("invocations = %+v, want a build before the spawn when a .swift file is newer than the runner app", invocations)
	}
}

func TestXctestLauncher_EnsureRunning_ProcessExitsEarly_ReturnsSpawnExitError(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnExit: true, spawnExitStderr: "xcodebuild: no matching destination"}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	_, err := l.EnsureRunning(context.Background())
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) {
		t.Fatalf("EnsureRunning() error = %v (%T), want *iosdriver.XctestLauncherError", err, err)
	}
	if launchErr.Code != iosdriver.XctestErrSpawnExit {
		t.Fatalf("Code = %q, want %q", launchErr.Code, iosdriver.XctestErrSpawnExit)
	}
	if !strings.Contains(launchErr.Message, "no matching destination") {
		t.Fatalf("Message = %q, want the captured xcodebuild stderr tail", launchErr.Message)
	}
}

func TestXctestLauncher_EnsureRunning_ProcessNotFound_ReturnsNoXcodeError(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnStartErr: &exec.Error{Name: "xcodebuild", Err: exec.ErrNotFound}}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	_, err := l.EnsureRunning(context.Background())
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) {
		t.Fatalf("EnsureRunning() error = %v (%T), want *iosdriver.XctestLauncherError", err, err)
	}
	if launchErr.Code != iosdriver.XctestErrNoXcode {
		t.Fatalf("Code = %q, want %q", launchErr.Code, iosdriver.XctestErrNoXcode)
	}
}

// TestXctestLauncher_EnsureRunning_BuildsThenSpawnsWithExactArgv_CustomIdentifiers
// guards LaunchConfig's config-driven identifiers: setting Scheme and
// OnlyTestingTarget must route those values into both the build and
// the spawn xcodebuild invocations instead of the standard Atomyx
// driver defaults, so a consumer can rename the underlying Xcode
// project without editing the launcher. The companion default-argv
// tests above (for example
// TestXctestLauncher_EnsureRunning_BuildsThenSpawnsWithExactArgv_Simulator)
// already prove that an empty LaunchConfig still resolves to those
// same defaults.
func TestXctestLauncher_EnsureRunning_BuildsThenSpawnsWithExactArgv_CustomIdentifiers(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir:        projectDir,
		Port:              port,
		Kind:              iosdriver.KindSimulator,
		UDID:              "SIM-1",
		Scheme:            "CustomScheme",
		OnlyTestingTarget: "CustomAgent/CustomAgent/testServeCommands",
	})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("invocations = %+v, want exactly one build then one spawn", invocations)
	}

	buildArgs := invocations[0].Args
	if !containsArgPair(buildArgs, "-scheme", "CustomScheme") {
		t.Fatalf("build args = %+v, want -scheme CustomScheme", buildArgs)
	}
	if containsArgPair(buildArgs, "-scheme", "AtomyxDriver") {
		t.Fatalf("build args = %+v, want the configured scheme, not the default", buildArgs)
	}

	spawnArgs := invocations[1].Args
	if !containsArgPair(spawnArgs, "-scheme", "CustomScheme") {
		t.Fatalf("spawn args = %+v, want -scheme CustomScheme", spawnArgs)
	}
	wantOnlyTesting := "-only-testing:CustomAgent/CustomAgent/testServeCommands"
	if !containsArg(spawnArgs, wantOnlyTesting) {
		t.Fatalf("spawn args = %+v, want %q", spawnArgs, wantOnlyTesting)
	}
	if containsArg(spawnArgs, "-only-testing:AtomyxDriverAgent/AtomyxDriverAgent/testServeCommands") {
		t.Fatalf("spawn args = %+v, want the configured testing target, not the default", spawnArgs)
	}
}

// containsArgPair reports whether args contains flag immediately
// followed by value, the shape xcodebuild's "-flag value" pairs take.
func containsArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// containsArg reports whether args contains value as a standalone
// element, the shape a single-token flag like "-only-testing:target"
// takes.
func containsArg(args []string, value string) bool {
	for _, a := range args {
		if a == value {
			return true
		}
	}
	return false
}

func TestXctestLauncher_EnsureRunning_TimesOutWhenDriverNeverBecomesReachable(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: false}
	l := iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, iosdriver.LaunchConfig{
		ProjectDir:      projectDir,
		Port:            port,
		Kind:            iosdriver.KindSimulator,
		UDID:            "SIM-1",
		LaunchTimeoutMs: 1_500,
	})

	_, err := l.EnsureRunning(context.Background())
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) {
		t.Fatalf("EnsureRunning() error = %v (%T), want *iosdriver.XctestLauncherError", err, err)
	}
	if launchErr.Code != iosdriver.XctestErrLaunchTimeout {
		t.Fatalf("Code = %q, want %q", launchErr.Code, iosdriver.XctestErrLaunchTimeout)
	}
}

// cancelOnSleepClock cancels a context the first time the readiness
// loop sleeps, so a test cancels the wait at a deterministic point
// instead of racing a timer.
type cancelOnSleepClock struct {
	cancel context.CancelFunc
}

func (c *cancelOnSleepClock) NowMs() int64 { return 0 }

func (c *cancelOnSleepClock) Sleep(ctx context.Context, _ int64) error {
	c.cancel()
	return ctx.Err()
}

var _ clock.Clock = (*cancelOnSleepClock)(nil)

func TestXctestLauncher_EnsureRunning_CanceledDuringReadinessWait_KillsSpawnedXcodebuild(t *testing.T) {
	projectDir, _ := newTestProjectDir(t)
	createFreshRunnerApp(t, projectDir, iosdriver.KindSimulator, time.Now())
	port := freeTCPPort(t)
	spawnReturned := make(chan struct{})
	runner := &fakeXcodebuildRunner{port: port, spawnReady: false, spawnReturned: spawnReturned}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := iosdriver.NewXctestLauncher(runner, runner, &cancelOnSleepClock{cancel: cancel}, iosdriver.LaunchConfig{
		ProjectDir: projectDir,
		Port:       port,
		Kind:       iosdriver.KindSimulator,
		UDID:       "SIM-1",
	})

	_, err := l.EnsureRunning(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureRunning() error = %v, want context.Canceled", err)
	}
	select {
	case <-spawnReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("the spawned xcodebuild process was still running after the launch was canceled")
	}
}
