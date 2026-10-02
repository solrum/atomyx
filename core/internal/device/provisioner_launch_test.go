package device

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

const (
	testRunnerPidBase = 9100
	testBootSeconds   = 1759372800
)

// testProcessStart is the start time the fixture's inspector reports for
// every launched runner process.
var testProcessStart = time.Date(2026, time.October, 2, 10, 15, 2, 0, time.UTC)

// runnerFarm is a process.BackgroundRunner that models the processes a
// launch starts. A started xcodebuild records the environment the
// runner would read. A started iproxy opens a listener on its host port
// that answers the ping handshake as the runner behind the tunnel. So a
// device runner is reachable only after the provisioner has started the
// tunnel, exactly as on hardware.
type runnerFarm struct {
	t *testing.T

	mu          sync.Mutex
	invocations []process.FakeBackgroundInvocation
	procs       []*farmProcess
	runnerEnv   []string
	nextPid     int
	// inspector, when set, learns every started process so a record of it
	// can read its start time.
	inspector *process.FakeInspector
	// exitFirstRunners makes the first N started runners exit at once with
	// status 65, modeling a runner that lost the race for its port.
	exitFirstRunners int
}

type farmProcess struct {
	*process.FakeBackgroundProcess
	name     string
	listener net.Listener
}

func (p *farmProcess) Kill() error {
	if p.listener != nil {
		_ = p.listener.Close()
	}
	return p.FakeBackgroundProcess.Kill()
}

func newRunnerFarm(t *testing.T) *runnerFarm {
	t.Helper()
	return &runnerFarm{t: t, nextPid: testRunnerPidBase}
}

func (f *runnerFarm) Start(ctx context.Context, name string, args ...string) (process.BackgroundProcess, error) {
	f.mu.Lock()
	f.invocations = append(f.invocations, process.FakeBackgroundInvocation{Name: name, Args: append([]string(nil), args...), Env: append([]string(nil), process.EnvFrom(ctx)...)})
	f.nextPid++
	pid := f.nextPid
	if name == "xcodebuild" {
		f.runnerEnv = append([]string(nil), process.EnvFrom(ctx)...)
	}
	runnerEnv := append([]string(nil), f.runnerEnv...)
	f.mu.Unlock()

	proc := &farmProcess{FakeBackgroundProcess: process.NewFakeBackgroundProcess(), name: name}
	proc.SetPid(pid)
	if f.inspector != nil {
		f.inspector.Set(process.ProcessInfo{PID: pid, PGID: pid, Command: name, StartTime: testProcessStart})
	}
	switch name {
	case "iproxy":
		hostPort, _, _ := strings.Cut(args[0], ":")
		listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:"+hostPort)
		if err != nil {
			return nil, err
		}
		proc.listener = listener
		go serveIdentity(listener, identityFieldsOf(runnerEnv))
		f.t.Cleanup(func() { _ = listener.Close() })
	case "xcodebuild":
		f.mu.Lock()
		exitEarly := f.exitFirstRunners > 0
		if exitEarly {
			f.exitFirstRunners--
		}
		f.mu.Unlock()
		if exitEarly {
			proc.Exit(65)
		} else if envOf(runnerEnv, "TEST_RUNNER_ATOMYX_TARGET_UDID") == "" {
			port := envOf(runnerEnv, "TEST_RUNNER_ATOMYX_PORT")
			listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:"+port)
			if err != nil {
				return nil, err
			}
			proc.listener = listener
			go serveIdentity(listener, identityFieldsOf(runnerEnv))
			f.t.Cleanup(func() { _ = listener.Close() })
		}
	}
	f.mu.Lock()
	f.procs = append(f.procs, proc)
	f.mu.Unlock()
	return proc, nil
}

func (f *runnerFarm) started(name string) []*farmProcess {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*farmProcess
	for _, p := range f.procs {
		if p.name == name {
			out = append(out, p)
		}
	}
	return out
}

func (f *runnerFarm) Invocations() []process.FakeBackgroundInvocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]process.FakeBackgroundInvocation(nil), f.invocations...)
}

func envOf(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func identityFieldsOf(env []string) string {
	fields := fmt.Sprintf(`"runnerId":%q`, envOf(env, "TEST_RUNNER_ATOMYX_RUNNER_ID"))
	if udid := envOf(env, "TEST_RUNNER_ATOMYX_TARGET_UDID"); udid != "" {
		fields += fmt.Sprintf(`,"targetUdid":%q`, udid)
	} else {
		fields += `,"simulatorUdid":"SIM-1"`
	}
	return fields
}

// serveIdentity answers every line on every connection with a ping
// reply that echoes the request id and carries identityFields.
func serveIdentity(l net.Listener, identityFields string) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer func() { _ = c.Close() }()
			sc := bufio.NewScanner(c)
			for sc.Scan() {
				id := idOf(sc.Text())
				_, _ = fmt.Fprintf(c, `{"id":%s,"ok":true,"data":{"pong":true,%s}}`+"\n", id, identityFields)
			}
		}(c)
	}
}

func idOf(line string) string {
	_, rest, ok := strings.Cut(line, `"id":`)
	if !ok {
		return "0"
	}
	end := strings.IndexAny(rest, ",}")
	if end < 0 {
		return "0"
	}
	return rest[:end]
}

// launchFixture prepares a project directory whose runner product is
// current, so a launch spawns without building, and a process runner
// that answers the host identity queries the runner record needs.
type launchFixture struct {
	projectDir string
	stateDir   string
	runner     *process.Fake
	farm       *runnerFarm
	inspector  *process.FakeInspector
}

func newLaunchFixture(t *testing.T) *launchFixture {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "AtomyxDriver.xcodeproj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "AtomyxDriver.xcodeproj", "project.pbxproj"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	products := filepath.Join(projectDir, "build", "Build", "Products")
	for _, config := range []string{"Debug-iphonesimulator", "Debug-iphoneos"} {
		if err := os.MkdirAll(filepath.Join(products, config, "AtomyxDriverAgent-Runner.app"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, sdk := range []string{"iphonesimulator", "iphoneos"} {
		if err := os.WriteFile(filepath.Join(products, "AtomyxDriver_"+sdk+"27.0-arm64.xctestrun"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner := process.NewFake()
	inspector := process.NewFakeInspector(time.Unix(testBootSeconds, 0).UTC())
	farm := newRunnerFarm(t)
	farm.inspector = inspector
	return &launchFixture{projectDir: projectDir, stateDir: t.TempDir(), runner: runner, farm: farm, inspector: inspector}
}

func (f *launchFixture) provisioner(logger logging.Logger) *DriverProvisioner {
	return NewDriverProvisioner(DriverProvisionerDeps{
		Runner: f.runner, Background: f.farm, Clock: instantClock{}, Inspector: f.inspector,
		IosProjectDir: f.projectDir, IosDevTeam: "TEAM123", Logger: logger,
		RunnerStateDir: f.stateDir,
		RunnerStateKey: func(udid string) string { return "key-" + udid },
	})
}

func (f *launchFixture) statePath(udid string) string {
	return RunnerStatePath(f.stateDir, "key-"+udid)
}

// instantClock makes every sleep return immediately.
type instantClock struct{}

func (instantClock) NowMs() int64                             { return 0 }
func (instantClock) Sleep(ctx context.Context, _ int64) error { return ctx.Err() }

func TestDriverProvisioner_EnsureIOSAgent_Simulator_LaunchesOnAReservedPortAndRecordsTheRunner(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	fixedNow := time.Date(2026, time.October, 2, 11, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return fixedNow }

	port, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	if port == 0 || port == iosControlPort {
		t.Fatalf("port = %d, want a freshly allocated host port", port)
	}

	envs := fx.farm.Invocations()
	if len(envs) != 1 || envs[0].Name != "xcodebuild" {
		t.Fatalf("background starts = %+v, want exactly one xcodebuild", envs)
	}
	if got := envOf(envs[0].Env, "TEST_RUNNER_ATOMYX_PORT"); got != strconv.Itoa(port) {
		t.Errorf("runner port env = %q, want the returned port %d", got, port)
	}
	runnerID := envOf(envs[0].Env, "TEST_RUNNER_ATOMYX_RUNNER_ID")
	if runnerID == "" {
		t.Error("runner id env is empty, want a launch identifier the launcher can verify")
	}

	state, err := ReadRunnerState(fx.statePath("SIM-1"))
	if err != nil {
		t.Fatalf("runner record: %v, want it written at launch", err)
	}
	wantPid := testRunnerPidBase + 1
	want := RunnerState{
		UDID: "SIM-1", Kind: "simulator",
		RunnerID: runnerID, HostPort: port, Pid: wantPid, Pgid: wantPid,
		ProcessStartTime: testProcessStart,
		HostBootTime:     time.Unix(testBootSeconds, 0).UTC(),
		StartedAt:        fixedNow,
	}
	if !state.ProcessStartTime.Equal(want.ProcessStartTime) || !state.HostBootTime.Equal(want.HostBootTime) || !state.StartedAt.Equal(want.StartedAt) {
		t.Errorf("recorded times = %+v, want %+v", state, want)
	}
	if state.UDID != want.UDID || state.Kind != want.Kind || state.RunnerID != want.RunnerID || state.HostPort != want.HostPort || state.Pid != want.Pid || state.Pgid != want.Pgid {
		t.Errorf("record = %+v, want %+v", state, want)
	}
	if info, err := os.Stat(fx.statePath("SIM-1")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("record mode = %v (%v), want 0600", info, err)
	}

	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	if _, err := os.Stat(fx.statePath("SIM-1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record after kill: %v, want it removed", err)
	}
	if !fx.farm.started("xcodebuild")[0].Killed() {
		t.Error("kill() left the runner process running")
	}
	p.mu.Lock()
	held := len(p.reserved)
	p.mu.Unlock()
	if held != 0 {
		t.Errorf("reserved ports after kill = %d, want 0", held)
	}
}

func TestDriverProvisioner_EnsureIOSAgent_Device_StartsTheTunnelBeforeTheRunnerIsProbed(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)

	port, kill, err := p.EnsureIOSAgent(context.Background(), "DEV-1", IosAgentKindDevice)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v, want the runner reachable through the provisioner's own tunnel", err)
	}

	invs := fx.farm.Invocations()
	if len(invs) != 2 || invs[0].Name != "xcodebuild" || invs[1].Name != "iproxy" {
		t.Fatalf("background starts = %+v, want xcodebuild then iproxy", invs)
	}
	if want := []string{fmt.Sprintf("%d:22087", port), "-u", "DEV-1"}; strings.Join(invs[1].Args, " ") != strings.Join(want, " ") {
		t.Errorf("iproxy args = %v, want %v", invs[1].Args, want)
	}
	if got := envOf(invs[0].Env, "TEST_RUNNER_ATOMYX_PORT"); got != "22087" {
		t.Errorf("runner port env = %q, want the device-side port 22087", got)
	}
	if got := envOf(invs[0].Env, "TEST_RUNNER_ATOMYX_TARGET_UDID"); got != "DEV-1" {
		t.Errorf("target udid env = %q, want DEV-1", got)
	}
	if _, err := os.Stat(fx.statePath("DEV-1")); err != nil {
		t.Errorf("runner record: %v, want it written at launch", err)
	}

	if err := kill(); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	if !fx.farm.started("iproxy")[0].Killed() || !fx.farm.started("xcodebuild")[0].Killed() {
		t.Error("kill() left the tunnel or the runner running")
	}
	if _, err := os.Stat(fx.statePath("DEV-1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record after kill: %v, want it removed", err)
	}
}

func TestDriverProvisioner_EnsureIOSAgent_FailedLaunchLeavesNoRecordTunnelOrReservation(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	// The farm's simulator runner reports SIM-1, so asking for another
	// simulator makes every attempt fail the identity check.
	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-OTHER", IosAgentKindSimulator)
	if err == nil {
		_ = kill()
		t.Fatal("EnsureIOSAgent() error = nil, want an identity mismatch for a runner that reports another simulator")
	}
	if _, statErr := os.Stat(fx.statePath("SIM-OTHER")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("record after a failed launch: %v, want none", statErr)
	}
	p.mu.Lock()
	held := len(p.reserved)
	p.mu.Unlock()
	if held != 0 {
		t.Errorf("reserved ports after a failed launch = %d, want 0", held)
	}
	attempts := fx.farm.started("xcodebuild")
	if len(attempts) == 0 {
		t.Fatal("no runner was started")
	}
	for _, proc := range attempts {
		if !proc.Killed() {
			t.Error("a failed launch left a runner process running")
		}
	}
}

func TestDriverProvisioner_EnsureIOSAgent_WithoutAStateDirectoryWritesNoRecord(t *testing.T) {
	fx := newLaunchFixture(t)
	p := NewDriverProvisioner(DriverProvisionerDeps{
		Runner: fx.runner, Background: fx.farm, Clock: instantClock{}, IosProjectDir: fx.projectDir,
	})
	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	defer func() { _ = kill() }()
	entries, _ := os.ReadDir(fx.stateDir)
	if len(entries) != 0 {
		t.Errorf("state directory holds %v, want nothing", entries)
	}
}

func TestDriverProvisioner_Kill_KeepsTheRecordOfALaterRunner(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	later := RunnerState{RunnerID: "someone-else", HostPort: 1, Pid: 2, Pgid: 2}
	if err := WriteRunnerState(fx.statePath("SIM-1"), later); err != nil {
		t.Fatal(err)
	}
	_ = kill()
	got, err := ReadRunnerState(fx.statePath("SIM-1"))
	if err != nil || got.RunnerID != "someone-else" {
		t.Fatalf("record after kill = %+v (%v), want the later runner's record untouched", got, err)
	}
}

func TestDriverProvisioner_EnsureIOSAgent_ReportsProgressThroughTheLogger(t *testing.T) {
	fx := newLaunchFixture(t)
	logger := &recordingLogger{}
	p := fx.provisioner(logger)
	_, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v", err)
	}
	defer func() { _ = kill() }()
	if got := logger.messages(); len(got) != 1 || got[0] != "starting runner" {
		t.Fatalf("progress = %v, want [starting runner] for a launch with a current build", got)
	}
}

// recordingLogger collects Info messages, including those logged
// through a Child.
type recordingLogger struct {
	mu   sync.Mutex
	logs *[]string
}

func (l *recordingLogger) sink() *[]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logs == nil {
		l.logs = &[]string{}
	}
	return l.logs
}

func (l *recordingLogger) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logs == nil {
		return nil
	}
	return append([]string(nil), *l.logs...)
}

func (l *recordingLogger) Debug(string, logging.LogContext) {}
func (l *recordingLogger) Info(msg string, _ logging.LogContext) {
	sink := l.sink()
	l.mu.Lock()
	*sink = append(*sink, msg)
	l.mu.Unlock()
}
func (l *recordingLogger) Warn(string, logging.LogContext)         {}
func (l *recordingLogger) Error(string, error, logging.LogContext) {}
func (l *recordingLogger) Child(logging.LogContext) logging.Logger { return l }

func TestDriverProvisioner_ReservePort_NeverHandsOutAHeldPort(t *testing.T) {
	p := NewDriverProvisioner(DriverProvisionerDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})
	scripted := []int{40001, 40001, 40001, 40002}
	p.freePort = func() (int, error) {
		port := scripted[0]
		scripted = scripted[1:]
		return port, nil
	}

	first, err := p.reservePort()
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.reservePort()
	if err != nil {
		t.Fatal(err)
	}
	if first != 40001 || second != 40002 {
		t.Fatalf("reserved %d then %d, want 40001 then 40002 because the kernel re-offered a held port", first, second)
	}
	p.releasePorts(first)
	again := []int{40001}
	p.freePort = func() (int, error) { return again[0], nil }
	if got, err := p.reservePort(); err != nil || got != 40001 {
		t.Fatalf("reserve after release = %d (%v), want the released port 40001 reusable", got, err)
	}
}

func TestDriverProvisioner_ReservePort_GivesUpWhenEveryCandidateIsHeld(t *testing.T) {
	p := NewDriverProvisioner(DriverProvisionerDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})
	p.freePort = func() (int, error) { return 40001, nil }
	if _, err := p.reservePort(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.reservePort(); err == nil {
		t.Fatal("reservePort() error = nil, want a failure instead of a duplicate port")
	}
}

func TestDriverProvisioner_ReservePort_ConcurrentReservationsAreDistinct(t *testing.T) {
	p := NewDriverProvisioner(DriverProvisionerDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})
	const callers = 48
	ports := make(chan int, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			port, err := p.reservePort()
			if err != nil {
				t.Errorf("reservePort() error = %v", err)
				return
			}
			ports <- port
		}()
	}
	wg.Wait()
	close(ports)
	seen := map[int]bool{}
	for port := range ports {
		if seen[port] {
			t.Fatalf("port %d was handed out twice", port)
		}
		seen[port] = true
	}
}

func TestDriverProvisioner_EnsureIOSAgent_DeviceWithoutDevTeamReturnsTheSigningRemedy(t *testing.T) {
	fx := newLaunchFixture(t)
	t.Setenv("ATOMYX_DEV_TEAM", "")
	p := NewDriverProvisioner(DriverProvisionerDeps{
		Runner: fx.runner, Background: fx.farm, Clock: instantClock{}, IosProjectDir: fx.projectDir,
	})
	_, kill, err := p.EnsureIOSAgent(context.Background(), "DEV-1", IosAgentKindDevice)
	if kill == nil {
		t.Fatal("kill = nil, want a non-nil kill even on failure")
	}
	if err == nil || !strings.Contains(err.Error(), "ATOMYX_DEV_TEAM") {
		t.Fatalf("error = %v, want the ATOMYX_DEV_TEAM remedy", err)
	}
	if got := fx.farm.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want nothing launched without a development team", got)
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	port, err := operatingSystemFreePort()
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestDriverProvisioner_Kill_RunsItsWorkOnceSoALaterLaunchKeepsItsPort(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	shared := freeLoopbackPort(t)
	p.freePort = func() (int, error) { return shared, nil }

	_, killFirst, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatal(err)
	}
	if err := killFirst(); err != nil {
		t.Fatal(err)
	}
	_, killSecond, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = killSecond() }()

	if err := killFirst(); err != nil {
		t.Fatalf("second kill() error = %v", err)
	}

	p.mu.Lock()
	_, stillHeld := p.reserved[shared]
	p.mu.Unlock()
	if !stillHeld {
		t.Fatal("a repeated kill() released the port a later launch holds")
	}
}

func TestDriverProvisioner_EnsureIOSAgent_AFailedAttemptsPortIsFreeBeforeTheNextAllocation(t *testing.T) {
	fx := newLaunchFixture(t)
	fx.farm.exitFirstRunners = 1
	p := fx.provisioner(nil)
	first, second := freeLoopbackPort(t), freeLoopbackPort(t)
	ports := []int{first, second}
	var heldAtSecondAllocation bool
	calls := 0
	p.freePort = func() (int, error) {
		port := ports[calls]
		calls++
		if calls == 2 {
			p.mu.Lock()
			_, heldAtSecondAllocation = p.reserved[first]
			p.mu.Unlock()
		}
		return port, nil
	}

	port, kill, err := p.EnsureIOSAgent(context.Background(), "SIM-1", IosAgentKindSimulator)
	if err != nil {
		t.Fatalf("EnsureIOSAgent() error = %v, want the retry to succeed", err)
	}
	defer func() { _ = kill() }()

	if port != second {
		t.Fatalf("port = %d, want the second attempt's %d", port, second)
	}
	if heldAtSecondAllocation {
		t.Error("the failed first attempt's port was still reserved when the second attempt allocated")
	}
	p.mu.Lock()
	held := len(p.reserved)
	p.mu.Unlock()
	if held != 1 {
		t.Errorf("reserved ports = %d, want only the live runner's port", held)
	}
}

func TestDriverProvisioner_EnsureAndroidAgent_ForwardsEachSerialToItsOwnAllocatedHostPort(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "A", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41001\n"}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "B", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41002\n"}, nil)
	p := NewDriverProvisioner(DriverProvisionerDeps{
		Runner: runner, Background: process.NewFakeBackground(), Clock: instantClock{},
		AndroidAPKPath: filepath.Join(t.TempDir(), "missing.apk"),
	})

	// The launch fails at the missing APK, after the forward is made.
	_ = p.EnsureAndroidAgent(context.Background(), "A")
	_ = p.EnsureAndroidAgent(context.Background(), "B")

	var forwards []string
	for _, inv := range runner.Invocations() {
		if len(inv.Args) >= 4 && inv.Args[2] == "forward" && inv.Args[3] != "--list" {
			forwards = append(forwards, strings.Join(inv.Args, " "))
		}
	}
	want := []string{
		"-s A forward tcp:0 tcp:8765", "-s A forward tcp:41001 tcp:8765",
		"-s B forward tcp:0 tcp:8765", "-s B forward tcp:41002 tcp:8765",
	}
	if strings.Join(forwards, "|") != strings.Join(want, "|") {
		t.Fatalf("forwards = %v, want %v, so no serial shares a fixed host port", forwards, want)
	}
}
