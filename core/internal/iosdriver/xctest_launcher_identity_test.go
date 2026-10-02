package iosdriver_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"dev.atomyx/internal/iosdriver"
)

func requireLauncherCode(t *testing.T, err error, want iosdriver.XctestLauncherErrorCode) (exitCode int) {
	t.Helper()
	var launchErr *iosdriver.XctestLauncherError
	if !errors.As(err, &launchErr) {
		t.Fatalf("EnsureRunning() error = %v (%T), want *iosdriver.XctestLauncherError", err, err)
	}
	if launchErr.Code != want {
		t.Fatalf("Code = %q (%s), want %q", launchErr.Code, launchErr.Message, want)
	}
	return launchErr.ExitCode
}

func newSimLauncher(t *testing.T, runner *fakeXcodebuildRunner, cfg iosdriver.LaunchConfig) *iosdriver.XctestLauncher {
	t.Helper()
	projectDir, _ := newTestProjectDir(t)
	cfg.ProjectDir = projectDir
	if cfg.UDID == "" {
		cfg.UDID = "SIM-1"
	}
	if cfg.Kind == "" {
		cfg.Kind = iosdriver.KindSimulator
	}
	return iosdriver.NewXctestLauncher(runner, runner, &fakeClock{}, cfg)
}

func TestXctestLauncher_EnsureRunning_ExistingRunnerOfAnotherSimulator_IsRejected(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, `"simulatorUdid":"SIM-2"`)
	defer cleanup()
	runner := &fakeXcodebuildRunner{}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port})

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrRunnerMismatch)
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want nothing spawned next to a foreign runner", runner.Invocations())
	}
}

func TestXctestLauncher_EnsureRunning_ExistingRunnerWithOtherRunnerID_IsRejected(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, `"runnerId":"other","simulatorUdid":"SIM-1"`)
	defer cleanup()
	runner := &fakeXcodebuildRunner{}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port, RunnerID: "mine"})

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrRunnerMismatch)
}

func TestXctestLauncher_EnsureRunning_LegacyRunnerWithoutIdentity_IsRejected(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, ``)
	defer cleanup()
	runner := &fakeXcodebuildRunner{}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port})

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrRunnerMismatch)
}

func TestXctestLauncher_EnsureRunning_HandStartedSimulatorRunner_IsAdoptedByItsSimulatorUDID(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, `"simulatorUdid":"SIM-1"`)
	defer cleanup()
	runner := &fakeXcodebuildRunner{}
	var warnings []string
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port, Warn: func(m string) { warnings = append(warnings, m) }})

	if _, err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v, want the runner of the requested simulator adopted", err)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want none when the runner is adopted", runner.Invocations())
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one warning about the missing launch id", warnings)
	}
}

func TestXctestLauncher_EnsureRunning_PassesLaunchEnvironment_Simulator(t *testing.T) {
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{spawnReady: true}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port, RunnerID: "r-1"})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	defer func() { _ = kill() }()

	envs := runner.SpawnEnvs()
	if len(envs) != 1 {
		t.Fatalf("spawns = %d, want 1", len(envs))
	}
	if got := envValue(envs[0], "TEST_RUNNER_ATOMYX_PORT"); got != strconv.Itoa(port) {
		t.Errorf("TEST_RUNNER_ATOMYX_PORT = %q, want the host port %d a simulator binds itself", got, port)
	}
	if got := envValue(envs[0], "TEST_RUNNER_ATOMYX_RUNNER_ID"); got != "r-1" {
		t.Errorf("TEST_RUNNER_ATOMYX_RUNNER_ID = %q, want r-1", got)
	}
	if got := envValue(envs[0], "TEST_RUNNER_ATOMYX_TARGET_UDID"); got != "" {
		t.Errorf("TEST_RUNNER_ATOMYX_TARGET_UDID = %q, want it unset for a simulator", got)
	}
}

func TestXctestLauncher_EnsureRunning_PassesLaunchEnvironment_Device(t *testing.T) {
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{
		Port: port, Kind: iosdriver.KindDevice, UDID: "DEV-1", DevTeam: "TEAM", RunnerID: "r-1",
	})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}
	defer func() { _ = kill() }()

	env := runner.SpawnEnvs()[0]
	if got := envValue(env, "TEST_RUNNER_ATOMYX_PORT"); got != "22087" {
		t.Errorf("TEST_RUNNER_ATOMYX_PORT = %q, want the fixed device-side port 22087", got)
	}
	if got := envValue(env, "TEST_RUNNER_ATOMYX_TARGET_UDID"); got != "DEV-1" {
		t.Errorf("TEST_RUNNER_ATOMYX_TARGET_UDID = %q, want DEV-1", got)
	}
}

func TestXctestLauncher_EnsureRunning_LaunchedRunnerWithOtherIdentity_IsRejectedAndKilled(t *testing.T) {
	port := freeTCPPort(t)
	returned := make(chan struct{})
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true, reportRunnerID: "someone-else", spawnReturned: returned}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port, RunnerID: "mine"})

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrRunnerMismatch)
	<-returned
	if got := len(runner.SpawnEnvs()); got != 1 {
		t.Fatalf("spawns = %d, want 1 because a fixed port cannot be retried", got)
	}
}

func sequencePorts(t *testing.T, ports ...int) func() (int, error) {
	t.Helper()
	next := 0
	return func() (int, error) {
		if next >= len(ports) {
			t.Fatalf("port allocator called %d times, only %d ports were scripted", next+1, len(ports))
		}
		p := ports[next]
		next++
		return p, nil
	}
}

func TestXctestLauncher_EnsureRunning_RetriesOnExit65WithAFreshPort(t *testing.T) {
	p1, p2, p3 := freeTCPPort(t), freeTCPPort(t), freeTCPPort(t)
	runner := &fakeXcodebuildRunner{spawnReady: true, exitFirstSpawns: 2}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{AllocatePort: sequencePorts(t, p1, p2, p3)})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v, want success on the third attempt", err)
	}
	defer func() { _ = kill() }()

	envs := runner.SpawnEnvs()
	if len(envs) != 3 {
		t.Fatalf("spawns = %d, want 3", len(envs))
	}
	for i, want := range []int{p1, p2, p3} {
		if got := envValue(envs[i], "TEST_RUNNER_ATOMYX_PORT"); got != strconv.Itoa(want) {
			t.Errorf("attempt %d port = %s, want %d", i+1, got, want)
		}
	}
	if l.Port() != p3 {
		t.Errorf("Port() = %d, want the port of the successful attempt %d", l.Port(), p3)
	}
}

func TestXctestLauncher_EnsureRunning_StopsRetryingAfterTheBound(t *testing.T) {
	ports := make([]int, 0, iosdriver.XctestMaxLaunchAttempts)
	for range iosdriver.XctestMaxLaunchAttempts {
		ports = append(ports, freeTCPPort(t))
	}
	runner := &fakeXcodebuildRunner{spawnReady: true, exitFirstSpawns: 100}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{AllocatePort: sequencePorts(t, ports...)})

	_, err := l.EnsureRunning(context.Background())
	if exitCode := requireLauncherCode(t, err, iosdriver.XctestErrSpawnExit); exitCode != 65 {
		t.Fatalf("ExitCode = %d, want 65", exitCode)
	}
	if got := len(runner.SpawnEnvs()); got != iosdriver.XctestMaxLaunchAttempts {
		t.Fatalf("spawns = %d, want exactly %d", got, iosdriver.XctestMaxLaunchAttempts)
	}
}

func TestXctestLauncher_EnsureRunning_Exit65WithoutAllocatorIsNotRetried(t *testing.T) {
	port := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{port: port, spawnReady: true, exitFirstSpawns: 100}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{Port: port})

	_, err := l.EnsureRunning(context.Background())
	requireLauncherCode(t, err, iosdriver.XctestErrSpawnExit)
	if got := len(runner.SpawnEnvs()); got != 1 {
		t.Fatalf("spawns = %d, want 1 because a fixed port cannot change the outcome", got)
	}
}

func TestXctestLauncher_EnsureRunning_RetriesWhenAnotherRunnerHoldsTheAllocatedPort(t *testing.T) {
	occupied, cleanup := startFakeRunnerListener(t, true, `"simulatorUdid":"SIM-2"`)
	defer cleanup()
	free := freeTCPPort(t)
	runner := &fakeXcodebuildRunner{spawnReady: true}
	l := newSimLauncher(t, runner, iosdriver.LaunchConfig{AllocatePort: sequencePorts(t, occupied, free)})

	kill, err := l.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning() error = %v, want a retry on a fresh port", err)
	}
	defer func() { _ = kill() }()
	if l.Port() != free {
		t.Fatalf("Port() = %d, want %d", l.Port(), free)
	}
}
