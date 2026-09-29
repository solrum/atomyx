package androiddriver_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// fakeHealthProber answers a scripted sequence of /health results.
// Each call to Health consumes the next entry; once the sequence is
// exhausted, the last entry repeats, so a test can express "not ready
// for N attempts, then ready" without enumerating every poll.
type fakeHealthProber struct {
	mu        sync.Mutex
	responses []healthResult
	calls     int
}

type healthResult struct {
	resp transport.HealthResponse
	err  error
}

func newFakeHealthProber(responses ...healthResult) *fakeHealthProber {
	return &fakeHealthProber{responses: responses}
}

func (f *fakeHealthProber) Health(context.Context) (transport.HealthResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.responses) == 0 {
		return transport.HealthResponse{}, nil
	}
	idx := f.calls - 1
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	r := f.responses[idx]
	return r.resp, r.err
}

func (f *fakeHealthProber) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var _ androiddriver.HealthProber = (*fakeHealthProber)(nil)

func healthy() healthResult {
	return healthResult{resp: transport.HealthResponse{Ok: true, AccessibilityConnected: true}}
}

func notReady() healthResult {
	return healthResult{resp: transport.HealthResponse{Ok: false}}
}

// fakeClock is a deterministic clock.Clock: Sleep advances the
// current time immediately instead of blocking, so a test exercises
// the settle pauses, the poll loop, and the launch timeout without
// waiting on real time.
type fakeClock struct {
	mu      sync.Mutex
	current int64
	sleeps  []int64
}

func newFakeClock(startMs int64) *fakeClock {
	return &fakeClock{current: startMs}
}

func (c *fakeClock) NowMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *fakeClock) Sleep(ctx context.Context, ms int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ms > 0 {
		c.current += ms
		c.sleeps = append(c.sleeps, ms)
	}
	return nil
}

var _ clock.Clock = (*fakeClock)(nil)

func newTestAPK(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app-debug.apk")
	if err := os.WriteFile(path, []byte("apk"), 0o600); err != nil {
		t.Fatalf("write test apk: %v", err)
	}
	return path
}

func TestAgentLauncher_EnsureRunning_AlreadyHealthy_ShortCircuitsBeforeInstall(t *testing.T) {
	runner := process.NewFake()
	prober := newFakeHealthProber(healthy())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1"})

	if err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) != 1 {
		t.Fatalf("adb invocations = %+v, want exactly one forward call", invocations)
	}
	if !equalArgs(invocations[0].Args, []string{"-s", "S1", "forward", "tcp:8765", "tcp:8765"}) {
		t.Fatalf("adb forward args = %v", invocations[0].Args)
	}
	if got := prober.callCount(); got != 1 {
		t.Fatalf("health probe calls = %d, want 1 (short-circuit)", got)
	}
}

func TestAgentLauncher_EnsureRunning_ForwardFailure_ReturnsErrorWithoutProbing(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:8765", "tcp:8765"), process.Result{}, errors.New("device offline"))
	prober := newFakeHealthProber(healthy())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1"})

	if err := l.EnsureRunning(context.Background()); err == nil {
		t.Fatal("EnsureRunning() error = nil, want the forward failure surfaced")
	}
	if got := prober.callCount(); got != 0 {
		t.Fatalf("health probe calls = %d, want 0 when forward fails", got)
	}
}

func TestAgentLauncher_EnsureRunning_NotHealthy_InstallsRebindsStartsThenPolls(t *testing.T) {
	apk := newTestAPK(t)
	runner := process.NewFake()
	prober := newFakeHealthProber(notReady(), notReady(), healthy())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{
		Serial:  "S1",
		APKPath: apk,
	})

	if err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	wantArgs := [][]string{
		{"-s", "S1", "forward", "tcp:8765", "tcp:8765"},
		{"-s", "S1", "install", "-r", apk},
		{"-s", "S1", "shell", "settings", "delete", "secure", "enabled_accessibility_services"},
		{"-s", "S1", "shell", "settings", "put", "secure", "enabled_accessibility_services", "dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService"},
		{"-s", "S1", "shell", "am", "start-foreground-service", "-n", "dev.atomyx.agent/.control.AtomyxForegroundService"},
	}
	if len(invocations) != len(wantArgs) {
		t.Fatalf("adb invocations = %+v, want %d calls", invocations, len(wantArgs))
	}
	for i, want := range wantArgs {
		if invocations[i].Name != "adb" || !equalArgs(invocations[i].Args, want) {
			t.Fatalf("invocation[%d] = %+v, want args %v", i, invocations[i], want)
		}
	}

	settleCount := 0
	for _, ms := range clock.sleeps {
		if ms == 1000 {
			settleCount++
		}
	}
	if settleCount != 2 {
		t.Fatalf("1000ms settle sleeps = %d, want 2 (one after delete, one after put)", settleCount)
	}
	if prober.callCount() < 2 {
		t.Fatalf("health probe calls = %d, want at least 2 (pre-install + poll)", prober.callCount())
	}
}

func TestAgentLauncher_EnsureRunning_InstallFailure_ReturnsInstallFailedErrorAndStopsPipeline(t *testing.T) {
	apk := newTestAPK(t)
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "install", "-r", apk), process.Result{Stderr: "INSTALL_FAILED_INSUFFICIENT_STORAGE"}, errors.New("exit status 1"))
	prober := newFakeHealthProber(notReady())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1", APKPath: apk})

	err := l.EnsureRunning(context.Background())
	var launcherErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launcherErr) {
		t.Fatalf("EnsureRunning() error = %v, want *AgentLauncherError", err)
	}
	if launcherErr.Code != androiddriver.ErrCodeInstallFailed {
		t.Fatalf("error code = %q, want %q", launcherErr.Code, androiddriver.ErrCodeInstallFailed)
	}
	if got := len(runner.Invocations()); got != 2 {
		t.Fatalf("adb invocations = %d, want 2 (forward, install) with no rebind/start", got)
	}
}

func TestAgentLauncher_EnsureRunning_AdbNotFound_ReturnsNoAdbErrorAndStopsPipeline(t *testing.T) {
	apk := newTestAPK(t)
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "install", "-r", apk), process.Result{}, &exec.Error{Name: "adb", Err: exec.ErrNotFound})
	prober := newFakeHealthProber(notReady())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1", APKPath: apk})

	err := l.EnsureRunning(context.Background())
	var launcherErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launcherErr) {
		t.Fatalf("EnsureRunning() error = %v, want *AgentLauncherError", err)
	}
	if launcherErr.Code != androiddriver.ErrCodeNoAdb {
		t.Fatalf("error code = %q, want %q", launcherErr.Code, androiddriver.ErrCodeNoAdb)
	}
	wantMessage := "adb not found on PATH. Install Android platform-tools."
	if launcherErr.Message != wantMessage {
		t.Fatalf("error message = %q, want %q", launcherErr.Message, wantMessage)
	}
	if got := len(runner.Invocations()); got != 2 {
		t.Fatalf("adb invocations = %d, want 2 (forward, install) with no rebind/start", got)
	}
}

func TestAgentLauncher_EnsureRunning_RebindFailure_ReturnsRebindFailedErrorAndStopsBeforeStart(t *testing.T) {
	apk := newTestAPK(t)
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "settings", "delete", "secure", "enabled_accessibility_services"), process.Result{}, errors.New("permission denied"))
	prober := newFakeHealthProber(notReady())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1", APKPath: apk})

	err := l.EnsureRunning(context.Background())
	var launcherErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launcherErr) {
		t.Fatalf("EnsureRunning() error = %v, want *AgentLauncherError", err)
	}
	if launcherErr.Code != androiddriver.ErrCodeRebindFailed {
		t.Fatalf("error code = %q, want %q", launcherErr.Code, androiddriver.ErrCodeRebindFailed)
	}
	if got := len(runner.Invocations()); got != 3 {
		t.Fatalf("adb invocations = %d, want 3 (forward, install, failed settings delete) with no start", got)
	}
}

func TestAgentLauncher_EnsureRunning_NeverHealthy_TimesOutAfter30sOfFakeTime(t *testing.T) {
	apk := newTestAPK(t)
	runner := process.NewFake()
	prober := newFakeHealthProber(notReady())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1", APKPath: apk})

	err := l.EnsureRunning(context.Background())
	var launcherErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launcherErr) {
		t.Fatalf("EnsureRunning() error = %v, want *AgentLauncherError", err)
	}
	if launcherErr.Code != androiddriver.ErrCodeLaunchTimeout {
		t.Fatalf("error code = %q, want %q", launcherErr.Code, androiddriver.ErrCodeLaunchTimeout)
	}
	if clock.NowMs() < 30_000 {
		t.Fatalf("fake clock advanced to %dms, want at least 30000ms", clock.NowMs())
	}
}

func TestAgentLauncher_EnsureRunning_CustomLaunchTimeout_TimesOutAtThatBudget(t *testing.T) {
	apk := newTestAPK(t)
	runner := process.NewFake()
	prober := newFakeHealthProber(notReady())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{
		Serial:          "S1",
		APKPath:         apk,
		LaunchTimeoutMs: 1_000,
	})

	err := l.EnsureRunning(context.Background())
	var launcherErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launcherErr) {
		t.Fatalf("EnsureRunning() error = %v, want *AgentLauncherError", err)
	}
	if launcherErr.Code != androiddriver.ErrCodeLaunchTimeout {
		t.Fatalf("error code = %q, want %q", launcherErr.Code, androiddriver.ErrCodeLaunchTimeout)
	}
	// Two 1000ms accessibility settle sleeps run before the timeout
	// window starts, so the clock lands at 2000ms (rebind) + 1000ms
	// (launch timeout) rather than at the timeout budget alone.
	const wantFinalMs = 2*1_000 + 1_000
	if clock.NowMs() != wantFinalMs {
		t.Fatalf("fake clock advanced to %dms, want exactly %dms", clock.NowMs(), wantFinalMs)
	}
}

func TestAgentLauncher_EnsureRunning_APKPathResolution_EnvOverrideUsedWhenOptionEmpty(t *testing.T) {
	apk := newTestAPK(t)
	t.Setenv("ATOMYX_ANDROID_APK", apk)

	runner := process.NewFake()
	// notReady first so the pipeline proceeds past the health
	// short-circuit into install, which carries the resolved APK path.
	prober := newFakeHealthProber(notReady(), healthy())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1"})

	if err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) < 2 {
		t.Fatalf("adb invocations = %+v, want at least forward+install", invocations)
	}
	if !equalArgs(invocations[1].Args, []string{"-s", "S1", "install", "-r", apk}) {
		t.Fatalf("install args = %v, want the ATOMYX_ANDROID_APK path %q", invocations[1].Args, apk)
	}
}

func TestAgentLauncher_EnsureRunning_APKPathResolution_OptionTakesPriorityOverEnv(t *testing.T) {
	envAPK := newTestAPK(t)
	optAPK := newTestAPK(t)
	t.Setenv("ATOMYX_ANDROID_APK", envAPK)

	runner := process.NewFake()
	prober := newFakeHealthProber(notReady(), healthy())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{
		Serial:  "S1",
		APKPath: optAPK,
	})

	if err := l.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	invocations := runner.Invocations()
	if len(invocations) < 2 || !equalArgs(invocations[1].Args, []string{"-s", "S1", "install", "-r", optAPK}) {
		t.Fatalf("install args = %+v, want the explicit APKPath %q to win over the env var", invocations, optAPK)
	}
}

func TestAgentLauncher_EnsureRunning_APKPathResolution_DefaultNotFound_ReturnsNoAPKError(t *testing.T) {
	t.Setenv("ATOMYX_ANDROID_APK", "")

	isolated := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(isolated); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	runner := process.NewFake()
	prober := newFakeHealthProber(notReady())
	clock := newFakeClock(0)
	l := androiddriver.NewAgentLauncher(runner, clock, prober, androiddriver.AgentLauncherOptions{Serial: "S1"})

	err = l.EnsureRunning(context.Background())
	var launcherErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launcherErr) {
		t.Fatalf("EnsureRunning() error = %v, want *AgentLauncherError", err)
	}
	if launcherErr.Code != androiddriver.ErrCodeNoAPK {
		t.Fatalf("error code = %q, want %q", launcherErr.Code, androiddriver.ErrCodeNoAPK)
	}
	wantSuffix := filepath.Join("platforms", "android-agent", "app", "build", "outputs", "apk", "debug", "app-debug.apk")
	if !strings.Contains(launcherErr.Message, wantSuffix) {
		t.Fatalf("error message = %q, want it to name the default relative APK path %q", launcherErr.Message, wantSuffix)
	}
}
