package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/wire"
)

func adoptingFactory(prov *FakeProvisioner, probe func(context.Context, driver.Driver) error) *DriverFactory {
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), Provisioner: prov})
	f.probeAdopted = probe
	return f
}

var simDescriptor = Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator}

func TestDriverFactory_Build_AdoptedRunnerThatAnswersTheProbeIsKept(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	prov.SetIOSAdopted("SIM-1", true)
	probes := 0
	f := adoptingFactory(prov, func(context.Context, driver.Driver) error { probes++; return nil })

	built, err := f.Build(context.Background(), simDescriptor)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Release()

	if probes != 1 {
		t.Fatalf("probes = %d, want exactly one after Connect", probes)
	}
	if opts := prov.IOSOptions(); len(opts) != 1 || opts[0].Fresh {
		t.Fatalf("provision options = %+v, want one call that allows adoption", opts)
	}
	if got := prov.IOSKillCount("SIM-1"); got != 0 {
		t.Fatalf("kill count = %d, want the adopted runner kept", got)
	}
}

func TestDriverFactory_Build_AdoptedRunnerThatFailsTheProbeIsEndedAndReplacedByAFreshOne(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	prov.SetIOSAdopted("SIM-1", true)
	probes := 0
	f := adoptingFactory(prov, func(context.Context, driver.Driver) error {
		probes++
		return errors.New("the runner stopped answering")
	})

	built, err := f.Build(context.Background(), simDescriptor)
	if err != nil {
		t.Fatalf("Build() error = %v, want the fresh runner used after the adopted one failed the probe", err)
	}
	defer built.Release()

	if probes != 1 {
		t.Fatalf("probes = %d, want the fresh runner not probed", probes)
	}
	opts := prov.IOSOptions()
	if len(opts) != 2 || opts[0].Fresh || !opts[1].Fresh {
		t.Fatalf("provision options = %+v, want an adopting call then a fresh one", opts)
	}
	if got := prov.IOSKillCount("SIM-1"); got != 1 {
		t.Fatalf("kill count = %d, want the failed adopted runner ended once", got)
	}
}

func TestDriverFactory_Build_AdoptedRunnerThatCannotConnectIsReplacedOnceThenTheFailureIsReported(t *testing.T) {
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", hangupPort(t))
	prov.SetIOSAdopted("SIM-1", true)
	f := adoptingFactory(prov, func(context.Context, driver.Driver) error {
		t.Fatal("the probe ran although Connect failed")
		return nil
	})

	if _, err := f.Build(context.Background(), simDescriptor); err == nil {
		t.Fatal("Build() error = nil, want the connect failure of the fresh runner")
	}

	opts := prov.IOSOptions()
	if len(opts) != 2 || !opts[1].Fresh {
		t.Fatalf("provision options = %+v, want exactly one fresh retry", opts)
	}
	if got := prov.IOSKillCount("SIM-1"); got != 2 {
		t.Fatalf("kill count = %d, want both runners ended", got)
	}
}

func TestDriverFactory_Build_LaunchedRunnerIsNotProbed(t *testing.T) {
	port := serveIdentityOn(t, `"simulatorUdid":"SIM-1"`)
	prov := NewFakeProvisioner()
	prov.SetIOSPort("SIM-1", port)
	f := adoptingFactory(prov, func(context.Context, driver.Driver) error {
		t.Fatal("a runner launched by this call was probed")
		return nil
	})

	built, err := f.Build(context.Background(), simDescriptor)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	built.Release()
}

type hierarchyDriver struct {
	driver.Driver
	deadline time.Time
	hasLimit bool
	err      error
}

func (d *hierarchyDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	d.deadline, d.hasLimit = ctx.Deadline()
	return wire.TreeNodeWire{}, d.err
}

func TestProbeHierarchy_BoundsTheCallAndReportsItsError(t *testing.T) {
	failure := errors.New("no answer")
	d := &hierarchyDriver{err: failure}

	err := probeHierarchy(context.Background(), d)

	if !errors.Is(err, failure) {
		t.Fatalf("probeHierarchy() error = %v, want the hierarchy error", err)
	}
	if !d.hasLimit || time.Until(d.deadline) > adoptedRunnerProbeTimeout {
		t.Fatalf("hierarchy deadline = %v (set %v), want one within %v", d.deadline, d.hasLimit, adoptedRunnerProbeTimeout)
	}
}
