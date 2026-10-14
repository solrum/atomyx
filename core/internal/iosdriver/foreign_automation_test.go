package iosdriver_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

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
	return d
}

func TestForeignAutomation_Simulator_ReportsLiveForeignRunnersOnly(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcrun", "simctl", "spawn", "UDID-1", "launchctl", "list"),
		process.Result{Stdout: readFixture(t, "simctl_launchctl_list.txt")}, nil)

	d := connectForeign(t, iosdriver.KindSimulator, runner)

	want := []driver.ForeignAutomationSession{
		{Name: "com.example.app.UITests.xctrunner", Kind: "XCUITest runner"},
		{Name: "com.facebook.WebDriverAgentRunner.xctrunner", Kind: "WebDriverAgent"},
	}
	if got := d.ForeignAutomation(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignAutomation() = %+v, want %+v", got, want)
	}
}

func TestForeignAutomation_Device_ReportsForeignRunnerBundlesOnly(t *testing.T) {
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
