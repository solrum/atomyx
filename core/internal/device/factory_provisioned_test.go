package device

import (
	"context"
	"errors"
	"net"
	"testing"

	"dev.atomyx/internal/process"
)

// coded is the shape of an error that carries a stable code, as
// diag.Coded does, so this package's tests read the code without
// importing diag.
type coded interface{ Code() string }

func codeOf(err error) string {
	var c coded
	if errors.As(err, &c) {
		return c.Code()
	}
	return ""
}

// serveIdentityOn starts a listener that answers the ping handshake
// with identityFields and returns its port.
func serveIdentityOn(t *testing.T, identityFields string) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go serveIdentity(l, identityFields)
	return l.Addr().(*net.TCPAddr).Port
}

// hangupPort returns a port with a listener that accepts connections
// and closes them at once, so a connect succeeds and the first request
// fails without any retry wait.
func hangupPort(t *testing.T) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func TestDriverFactory_Build_Provisioned_DialsTheProvisionedPortAndStopsTheRunnerOnDisconnect(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	bg := process.NewFakeBackground()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: bg, Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator})
	if err != nil {
		t.Fatalf("Build() error = %v, want the driver connected through the provisioned port", err)
	}
	if calls := prov.IOSCalls(); len(calls) != 1 || calls[0] != (FakeIOSCall{UDID: "SIM-1", Kind: IosAgentKindSimulator}) {
		t.Fatalf("provisioner calls = %+v, want one call for SIM-1 as a simulator", calls)
	}
	if got := prov.IOSKillCount("SIM-1"); got != 0 {
		t.Fatalf("kill count before Disconnect = %d, want 0", got)
	}

	if err := built.Driver.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	_ = built.Driver.Disconnect()
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("kill count after Disconnect = %d, want exactly 1", got)
	}
}

func TestDriverFactory_Build_ProvisionedDevice_LeavesTheTunnelToTheProvisioner(t *testing.T) {
	port := serveIdentityOn(t, `"targetUdid":"DEV-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("DEV-1", port)
	bg := process.NewFakeBackground()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: bg, Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{ID: "DEV-1", Platform: PlatformIOS, Kind: KindDevice})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := bg.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want no iproxy from the driver because the provisioner owns the tunnel", got)
	}
	_ = built.Driver.Disconnect()
	if got := prov.IOSKillCount("DEV-1"); got != 1 {
		t.Fatalf("kill count = %d, want 1", got)
	}
}

func TestDriverFactory_Build_ProvisioningFailureIsATypedAgentUnreachableError(t *testing.T) {
	prov := NewFakeProvisioner()
	prov.SetIOSResult("DEV-1", errors.New("signing is not configured"))
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	_, err := f.Build(context.Background(), Descriptor{ID: "DEV-1", Platform: PlatformIOS, Kind: KindDevice})
	if err == nil {
		t.Fatal("Build() error = nil, want the provisioning failure")
	}
	if got := codeOf(err); got != "AGENT_UNREACHABLE" {
		t.Fatalf("error code = %q, want AGENT_UNREACHABLE, the code a failed connect carries", got)
	}
}

func TestDriverFactory_Build_ConnectFailureAfterProvisioningStopsTheRunner(t *testing.T) {
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", hangupPort(t))
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	if _, err := f.Build(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator}); err == nil {
		t.Fatal("Build() error = nil, want a failure from an agent that hangs up")
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("kill count = %d, want 1 so a failed build leaks no runner", got)
	}
}

func TestDriverFactory_Build_DescriptorWithItsOwnPortIsNotProvisioned(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer func() { _ = built.Driver.Disconnect() }()
	if calls := prov.IOSCalls(); len(calls) != 0 {
		t.Fatalf("provisioner calls = %+v, want none for a caller that supplied its own runner port", calls)
	}
}

func TestDriverFactory_Build_HidPathRefusalAfterProvisioningStopsTheRunner(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-2"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	_, err := f.createDriver(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator, HidPort: 7777})
	if err == nil {
		t.Fatal("createDriver() error = nil, want the identity refusal")
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("kill count = %d, want 1 because the refused driver is never handed to a caller", got)
	}
}

func TestDriverFactory_Build_AndroidIgnoresTheProvisioner(t *testing.T) {
	prov := NewFakeProvisioner()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})
	if _, err := f.createDriver(context.Background(), Descriptor{ID: "emulator-5554", Platform: PlatformAndroid}); err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if len(prov.IOSCalls()) != 0 {
		t.Fatal("the iOS provisioner was called for an Android target")
	}
}

func TestDriverFactory_CreateDriver_DescriptorWithoutAnIDIsNotProvisioned(t *testing.T) {
	prov := NewFakeProvisioner()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	if _, err := f.createDriver(context.Background(), Descriptor{Platform: PlatformIOS, Kind: KindSimulator}); err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if calls := prov.IOSCalls(); len(calls) != 0 {
		t.Fatalf("provisioner calls = %+v, want none because no target is named", calls)
	}
}

func TestDriverFactory_Build_Provisioned_ReportsTheRunnerLossThroughBuiltLost(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	prov.FireLost("SIM-1", errors.New("exit status 1"))

	var exit *ProcessExitError
	if cause := <-built.Lost; !errors.As(cause, &exit) || exit.Component != LostRunner {
		t.Fatalf("Built.Lost delivered %v, want a runner *ProcessExitError", cause)
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("kill count after the loss = %d, want 1", got)
	}
	_ = built.Driver.Disconnect()
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("kill count after Disconnect = %d, want the kill path to run once", got)
	}
}
