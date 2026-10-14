package device

import (
	"context"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const (
	customProduct  = "/build/Debug-iphonesimulator/AtomyxDriverAgent-Runner.app"
	customBundleID = "com.acme.agent.xctrunner"
)

func stubCustomPlist(runner *process.Fake) {
	runner.Stub(
		process.ExactMatch("plutil", "-convert", "json", "-o", "-", customProduct+"/Info.plist"),
		process.Result{Stdout: `{"CFBundleIdentifier":"` + customBundleID + `"}`}, nil)
}

func TestReap_TerminatesTheBundleIdOfTheRecordedRunnerProduct(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	stubCustomPlist(f.runner)
	terminated := false
	f.runner.Stub(process.ExactMatch("xcrun", "simctl", "terminate", reapTestUDID, customBundleID), process.Result{}, nil)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		if terminated {
			return iosdriver.RunnerIdentity{}, false
		}
		terminated = true
		return iosdriver.RunnerIdentity{RunnerID: "runner-1", SimulatorUDID: reapTestUDID}, true
	}
	f.state.BuildProductPath = customProduct
	f.write()

	out := f.reap()

	if !out.TerminatedViaSimctl {
		t.Fatalf("Reap() = %+v, want the runner terminated through simctl with the custom bundle id", out)
	}
}

func TestReap_FallsBackToTheDefaultBundleIdWhenTheProductPlistIsUnreadable(t *testing.T) {
	f := newReapFixture(t, IosAgentKindSimulator)
	terminated := false
	f.runner.Stub(process.ExactMatch("xcrun", "simctl", "terminate", reapTestUDID, iosdriver.DefaultRunnerBundleID), process.Result{}, nil)
	f.answering = func() (iosdriver.RunnerIdentity, bool) {
		if terminated {
			return iosdriver.RunnerIdentity{}, false
		}
		terminated = true
		return iosdriver.RunnerIdentity{RunnerID: "runner-1", SimulatorUDID: reapTestUDID}, true
	}
	f.state.BuildProductPath = "/missing/Runner.app"
	f.write()

	out := f.reap()

	if !out.TerminatedViaSimctl {
		t.Fatalf("Reap() = %+v, want the default bundle id terminated", out)
	}
}

func TestDriverFactory_Build_ExcludesTheCustomRunnerFromForeignAutomation(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	prov.SetIOSBuildProduct("SIM-1", customProduct)
	runner := process.NewFake()
	stubCustomPlist(runner)
	runner.Stub(process.ExactMatch("xcrun", "simctl", "spawn", "SIM-1", "launchctl", "list"), process.Result{Stdout: "PID\tStatus\tLabel\n" +
		"4907\t0\tUIKitApplication:" + customBundleID + "[4b50][rb-legacy]\n" +
		"5058\t0\tUIKitApplication:dev.spike.uitests.xctrunner[d26e][rb-legacy]\n"}, nil)
	f := NewDriverFactory(DriverFactoryDeps{Runner: runner, Background: process.NewFakeBackground(), Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() { _ = built.Driver.Disconnect() })
	reporter, ok := driver.AsForeignAutomationReporter(built.Driver)
	if !ok {
		t.Fatal("driver does not report foreign automation")
	}

	deadline := time.Now().Add(5 * time.Second)
	var got []driver.ForeignAutomationSession
	for time.Now().Before(deadline) {
		if got = reporter.ForeignAutomation(); len(got) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 || got[0].Name != "dev.spike.uitests.xctrunner" {
		t.Fatalf("ForeignAutomation() = %+v, want only dev.spike.uitests.xctrunner", got)
	}
}
