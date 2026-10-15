package device

import (
	"context"
	"net"
	"strconv"
	"testing"

	"dev.atomyx/internal/process"
)

func TestDriverFactory_Build_VerifiedServedRunnerIsNeverProvisionedOrReleased(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{
		ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port, RunnerVerified: true,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if calls := prov.IOSCalls(); len(calls) != 0 {
		t.Fatalf("provisioner calls = %+v, want none for a served runner", calls)
	}

	if err := built.Driver.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	built.Release()
	built.Release()

	if got := prov.IOSKillCount("SIM-1"); got != 0 {
		t.Fatalf("kill count = %d, want 0 because Release never stops a runner Atomyx did not launch", got)
	}
	conn, err := new(net.Dialer).DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("served runner stopped answering after Release: %v", err)
	}
	_ = conn.Close()
}

func TestDriverFactory_Build_PhysicalDeviceReusesTheOperatorsTunnelAndLeavesItRunning(t *testing.T) {
	port := serveIdentityOn(t, `"targetUdid":"DEV-1"`)
	prov := NewFakeProvisioner()
	bg := process.NewFakeBackground()
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: bg, Provisioner: prov})

	built, err := f.Build(context.Background(), Descriptor{
		ID: "DEV-1", Platform: PlatformIOS, Kind: KindDevice, Port: port, RunnerVerified: true,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := bg.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want no iproxy because the tunnel already answers", got)
	}

	if err := built.Driver.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	built.Release()

	if calls := prov.IOSCalls(); len(calls) != 0 {
		t.Fatalf("provisioner calls = %+v, want none", calls)
	}
	conn, err := new(net.Dialer).DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("the operator's tunnel stopped answering after Disconnect and Release: %v", err)
	}
	_ = conn.Close()
}
