package session_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

type wrappedPlainFactory struct{ withOrchestra bool }

func (f wrappedPlainFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mockdriver.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	drv.StageHierarchy(wire.TreeNodeWire{})
	built := device.Built{Driver: drv}
	if f.withOrchestra {
		built.Orchestra = orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}})
	}
	return built, nil
}

type treeCountingDriver struct {
	driver.Driver
	trees *int
}

func (d treeCountingDriver) Hierarchy(ctx context.Context) (wire.TreeNodeWire, error) {
	*d.trees++
	return d.Driver.Hierarchy(ctx)
}

func TestWrapping_TheOrchestraActsThroughTheWrappedDriver(t *testing.T) {
	trees := 0
	wrap := func(d driver.Driver) driver.Driver { return treeCountingDriver{Driver: d, trees: &trees} }
	factory := sessiontest.Wrapping(wrappedPlainFactory{withOrchestra: true}).WithDriverWrap(wrap)

	built, err := factory.Build(context.Background(), device.Descriptor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := built.Orchestra.Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if trees != 1 {
		t.Fatalf("the wrapped driver saw %d hierarchy reads, want the Orchestra to act through it", trees)
	}
}

func TestWrapping_WithAnOrchestraBuilderUsesIt(t *testing.T) {
	built := 0
	rebuild := func(d driver.Driver) *orchestra.Orchestra {
		built++
		return orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.NewFakeClock(0)})
	}
	factory := sessiontest.WrappingWith(wrappedPlainFactory{withOrchestra: true}, rebuild).WithDriverWrap(func(d driver.Driver) driver.Driver { return d })

	if _, err := factory.Build(context.Background(), device.Descriptor{}); err != nil {
		t.Fatal(err)
	}
	if built != 1 {
		t.Fatalf("the builder ran %d times, want once", built)
	}
}

func TestWrapping_ABuiltWithoutAnOrchestraKeepsNone(t *testing.T) {
	factory := sessiontest.Wrapping(wrappedPlainFactory{}).WithDriverWrap(func(d driver.Driver) driver.Driver { return d })
	built, err := factory.Build(context.Background(), device.Descriptor{})
	if err != nil {
		t.Fatal(err)
	}
	if built.Orchestra != nil {
		t.Fatal("Wrapping invented an Orchestra the inner factory did not build")
	}
}
