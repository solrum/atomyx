package iosdriver_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const (
	ownBundleID = "dev.atomyx.driver.host.xctrunner"
	ownAppName  = "AtomyxDriverAgent-Runner"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func connectForeign(t *testing.T, kind iosdriver.Kind, runner *process.Fake) *iosdriver.IosDriver {
	t.Helper()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{
		Background: process.NewFakeBackground(), UDID: "UDID-1", Kind: kind,
		ExternalTunnel:    true,
		OwnRunnerBundleID: ownBundleID, OwnRunnerAppName: ownAppName,
	})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	d.WaitForeignAutomationProbe()
	return d
}

func TestForeignAutomation_Simulator_ReportsLiveForeignRunnersOnly(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcrun", "simctl", "spawn", "UDID-1", "launchctl", "list"),
		process.Result{Stdout: readFixture(t, "simctl_launchctl_list.txt")}, nil)

	d := connectForeign(t, iosdriver.KindSimulator, runner)

	want := []driver.ForeignAutomationSession{
		{Name: "dev.spike.uitests.xctrunner", Kind: "XCUITest runner"},
	}
	if got := d.ForeignAutomation(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignAutomation() = %+v, want %+v", got, want)
	}
}

func TestForeignAutomation_Simulator_ClassifiesWebDriverAgentRunner(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stdout: "812\t0\tUIKitApplication:com.facebook.WebDriverAgentRunner.xctrunner[e5f6][rb-legacy]\n"}

	d := connectForeign(t, iosdriver.KindSimulator, runner)

	want := []driver.ForeignAutomationSession{
		{Name: "com.facebook.WebDriverAgentRunner.xctrunner", Kind: "WebDriverAgent"},
	}
	if got := d.ForeignAutomation(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignAutomation() = %+v, want %+v", got, want)
	}
}

func TestForeignAutomation_Device_ReportsForeignRunnerBundlesOnly(t *testing.T) {
	// The fixture is shaped from the devicectl JSON envelope used elsewhere in
	// this package and awaits a capture from a real device.
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcrun", "devicectl", "device", "info", "processes", "--device", "UDID-1", "--json-output", "-"),
		process.Result{Stdout: readFixture(t, "devicectl_processes.json")}, nil)

	d := connectForeign(t, iosdriver.KindDevice, runner)

	want := []driver.ForeignAutomationSession{
		{Name: "ShopUITests-Runner", Kind: "XCUITest runner"},
		{Name: "WebDriverAgentRunner-Runner", Kind: "WebDriverAgent"},
	}
	if got := d.ForeignAutomation(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignAutomation() = %+v, want %+v", got, want)
	}
}

func TestForeignAutomation_ProbeFailureReportsNothingAndConnectSucceeds(t *testing.T) {
	for _, kind := range []iosdriver.Kind{iosdriver.KindSimulator, iosdriver.KindDevice} {
		runner := process.NewFake()
		runner.DefaultErr = errors.New("xcrun failed")
		d := connectForeign(t, kind, runner)
		if got := d.ForeignAutomation(); got != nil {
			t.Fatalf("kind %s: ForeignAutomation() = %+v, want nil", kind, got)
		}
	}
}

func TestForeignAutomation_MalformedDeviceOutputReportsNothing(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stdout: "not json"}
	d := connectForeign(t, iosdriver.KindDevice, runner)
	if got := d.ForeignAutomation(); got != nil {
		t.Fatalf("ForeignAutomation() = %+v, want nil", got)
	}
}

func TestForeignAutomation_ReturnsACopy(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stdout: readFixture(t, "simctl_launchctl_list.txt")}
	d := connectForeign(t, iosdriver.KindSimulator, runner)
	d.ForeignAutomation()[0].Name = "mutated"
	if got := d.ForeignAutomation()[0].Name; got == "mutated" {
		t.Fatal("ForeignAutomation() aliases its cache")
	}
}

func TestForeignAutomation_FoundThroughDriverAs(t *testing.T) {
	d := connectForeign(t, iosdriver.KindSimulator, process.NewFake())
	if _, ok := driver.As[driver.ForeignAutomationReporter](d); !ok {
		t.Fatal("IosDriver does not expose ForeignAutomationReporter")
	}
}

// gatedRunner holds every command until the test releases it or the
// command's context ends, which models a slow device listing.
type gatedRunner struct {
	mu      sync.Mutex
	stdout  string
	release chan struct{}
	started chan struct{}
	ended   chan error
}

func newGatedRunner(stdout string) *gatedRunner {
	return &gatedRunner{
		stdout:  stdout,
		release: make(chan struct{}),
		started: make(chan struct{}, 4),
		ended:   make(chan error, 4),
	}
}

func (g *gatedRunner) setOutput(stdout string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stdout = stdout
}

func (g *gatedRunner) Run(ctx context.Context, _ string, _ ...string) (process.Result, error) {
	g.started <- struct{}{}
	select {
	case <-g.release:
		g.mu.Lock()
		defer g.mu.Unlock()
		g.ended <- nil
		return process.Result{Stdout: g.stdout}, nil
	case <-ctx.Done():
		g.ended <- ctx.Err()
		return process.Result{}, ctx.Err()
	}
}

func awaitSignal[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

func newGatedForeignDriver(runner process.Runner) *iosdriver.IosDriver {
	return iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{
		Background: process.NewFakeBackground(), UDID: "UDID-1", Kind: iosdriver.KindSimulator,
		ExternalTunnel:    true,
		OwnRunnerBundleID: ownBundleID, OwnRunnerAppName: ownAppName,
	})
}

func TestForeignAutomation_ConnectDoesNotWaitForTheProbe(t *testing.T) {
	runner := newGatedRunner(readFixture(t, "simctl_launchctl_list.txt"))
	d := newGatedForeignDriver(runner)

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	awaitSignal(t, runner.started, "the probe to start")
	if got := d.ForeignAutomation(); got != nil {
		t.Fatalf("ForeignAutomation() = %+v while the probe is running, want nil", got)
	}

	close(runner.release)
	d.WaitForeignAutomationProbe()
	want := []driver.ForeignAutomationSession{{Name: "dev.spike.uitests.xctrunner", Kind: "XCUITest runner"}}
	if got := d.ForeignAutomation(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignAutomation() = %+v after the probe finished, want %+v", got, want)
	}
}

func TestForeignAutomation_ReconnectReplacesCachedSessions(t *testing.T) {
	runner := newGatedRunner(readFixture(t, "simctl_launchctl_list.txt"))
	close(runner.release)
	d := newGatedForeignDriver(runner)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	d.WaitForeignAutomationProbe()
	if got := d.ForeignAutomation(); len(got) != 1 || got[0].Name != "dev.spike.uitests.xctrunner" {
		t.Fatalf("ForeignAutomation() = %+v after the first connect", got)
	}

	runner.setOutput("900\t0\tUIKitApplication:com.example.other.xctrunner[aa11][rb-legacy]\n")
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	d.WaitForeignAutomationProbe()

	want := []driver.ForeignAutomationSession{{Name: "com.example.other.xctrunner", Kind: "XCUITest runner"}}
	if got := d.ForeignAutomation(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignAutomation() = %+v after reconnect, want %+v", got, want)
	}
}

func TestForeignAutomation_ReconnectCancelsTheOlderProbe(t *testing.T) {
	runner := newGatedRunner(readFixture(t, "simctl_launchctl_list.txt"))
	d := newGatedForeignDriver(runner)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	awaitSignal(t, runner.started, "the first probe to start")

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	if err := awaitSignal(t, runner.ended, "the first probe to end"); err == nil {
		t.Fatal("the first probe finished normally, want it cancelled by the reconnect")
	}
	awaitSignal(t, runner.started, "the second probe to start")
	close(runner.release)
	d.WaitForeignAutomationProbe()
	if got := d.ForeignAutomation(); len(got) != 1 {
		t.Fatalf("ForeignAutomation() = %+v, want the second probe's single session", got)
	}
}

func TestForeignAutomation_DisconnectCancelsTheProbe(t *testing.T) {
	runner := newGatedRunner(readFixture(t, "simctl_launchctl_list.txt"))
	d := newGatedForeignDriver(runner)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	awaitSignal(t, runner.started, "the probe to start")

	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if err := awaitSignal(t, runner.ended, "the probe to end"); !errors.Is(err, context.Canceled) {
		t.Fatalf("probe ended with %v, want context.Canceled", err)
	}
	d.WaitForeignAutomationProbe()
	if got := d.ForeignAutomation(); got != nil {
		t.Fatalf("ForeignAutomation() = %+v after Disconnect, want nil", got)
	}
}
