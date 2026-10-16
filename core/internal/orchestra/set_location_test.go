package orchestra

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

func TestSetLocation_PassesThePositionToTheDriver(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)
	want := driver.Location{Latitude: 10, Longitude: 20}

	if err := o.SetLocation(context.Background(), want); err != nil {
		t.Fatalf("SetLocation: %v", err)
	}
	if len(d.Locations) != 1 || d.Locations[0] != want {
		t.Fatalf("driver locations = %v, want [%v]", d.Locations, want)
	}
}

func TestSetLocation_RejectsAPositionOutOfRangeBeforeTheDriver(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)

	err := o.SetLocation(context.Background(), driver.Location{Latitude: 120})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(d.Locations) != 0 {
		t.Fatalf("driver locations = %v, want none", d.Locations)
	}
}

func TestSetLocation_ADeviceWithoutTheCapabilityIsUnsupported(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanSetLocation: false})
	o, _ := newTestOrchestra(d)

	err := o.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 1})
	if code, _, _ := diag.Describe(err); code != diag.CodeLocationUnsupported {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeLocationUnsupported)
	}
	if len(d.Locations) != 0 {
		t.Fatalf("driver locations = %v, want none", d.Locations)
	}
}

func TestTravel_SetsEachPointAfterTheTimeTheLegTakes(t *testing.T) {
	d := mock.New()
	o, clk := newTestOrchestra(d)
	points := []driver.Location{{Latitude: 0, Longitude: 0}, {Latitude: 0, Longitude: 0.01}, {Latitude: 0.01, Longitude: 0.01}}
	const speed = 20.0
	delays := driver.TravelLegDelaysMs(points, speed)

	if err := o.Travel(context.Background(), points, speed); err != nil {
		t.Fatalf("Travel: %v", err)
	}
	if len(d.Locations) != 3 || d.Locations[0] != points[0] || d.Locations[1] != points[1] || d.Locations[2] != points[2] {
		t.Fatalf("driver locations = %v, want %v", d.Locations, points)
	}
	if want := delays[0] + delays[1]; clk.NowMs() != want {
		t.Fatalf("clock advanced %dms, want %dms", clk.NowMs(), want)
	}
	if delays[0] < 50000 || delays[0] > 56000 {
		t.Fatalf("first leg delay = %dms, want about 55 seconds for 1.1 km at 20 m/s", delays[0])
	}
}

func TestTravel_RejectsAnInvalidRouteBeforeTheDriver(t *testing.T) {
	d := mock.New()
	o, clk := newTestOrchestra(d)

	err := o.Travel(context.Background(), []driver.Location{{Latitude: 1, Longitude: 1}}, 4)
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(d.Locations) != 0 || clk.NowMs() != 0 {
		t.Fatalf("driver locations = %v, clock = %d, want untouched", d.Locations, clk.NowMs())
	}
}

func TestTravel_ADeviceWithoutTheCapabilityIsUnsupported(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanSetLocation: false})
	o, _ := newTestOrchestra(d)

	err := o.Travel(context.Background(), []driver.Location{{}, {Latitude: 0.001}}, 4)
	if code, _, _ := diag.Describe(err); code != diag.CodeLocationUnsupported {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeLocationUnsupported)
	}
}

func TestTravel_AFailurePartWayStopsAtTheLastAcceptedPoint(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)
	points := []driver.Location{{}, {Latitude: 0.001}, {Latitude: 0.002}}
	d.SetLocationErr = errors.New("console closed")

	if err := o.Travel(context.Background(), points, 4); !errors.Is(err, d.SetLocationErr) {
		t.Fatalf("Travel error = %v, want the injected error", err)
	}
	if len(d.Locations) != 0 {
		t.Fatalf("driver locations = %v, want none recorded", d.Locations)
	}
}

func TestTravel_ACancelledContextStopsTheWait(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := o.Travel(ctx, []driver.Location{{}, {Latitude: 0.001}}, 4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Travel error = %v, want context.Canceled", err)
	}
}
