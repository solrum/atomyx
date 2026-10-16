package iosdriver_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

func newLocationSimulator(runner *process.Fake) *iosdriver.IosDriver {
	return iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
}

func TestIosDriver_SetLocation_SimulatorSendsLatitudeThenLongitude(t *testing.T) {
	tests := []struct {
		name     string
		location driver.Location
		pair     string
	}{
		{"northern and western", driver.Location{Latitude: 37.7749, Longitude: -122.4194}, "37.7749,-122.4194"},
		{"southern and eastern", driver.Location{Latitude: -33.8568, Longitude: 151.2153}, "-33.8568,151.2153"},
		{"whole numbers", driver.Location{Latitude: 10, Longitude: 20}, "10,20"},
		{"tiny values stay decimal", driver.Location{Latitude: 0.0000001, Longitude: 0}, "0.0000001,0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			d := newLocationSimulator(runner)

			if err := d.SetLocation(context.Background(), tc.location); err != nil {
				t.Fatalf("SetLocation() error = %v", err)
			}
			want := []process.Invocation{{Name: "xcrun", Args: []string{"simctl", "location", "SIM-1", "set", tc.pair}}}
			if got := runner.Invocations(); !reflect.DeepEqual(got, want) {
				t.Fatalf("invocations = %+v, want %+v", got, want)
			}
		})
	}
}

func TestIosDriver_SetLocation_SimulatorReportsTheCapability(t *testing.T) {
	if !newLocationSimulator(process.NewFake()).Capabilities().CanSetLocation {
		t.Fatal("Capabilities().CanSetLocation = false on a simulator, want true")
	}
}

func TestIosDriver_SetLocation_PhysicalDeviceIsUnsupported(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	if d.Capabilities().CanSetLocation {
		t.Fatal("Capabilities().CanSetLocation = true on a device, want false")
	}
	for _, location := range []driver.Location{{Latitude: 1, Longitude: 2}, {Latitude: 99}} {
		err := d.SetLocation(context.Background(), location)
		if code, _, _ := diag.Describe(err); code != diag.CodeLocationUnsupported {
			t.Fatalf("SetLocation(%+v) code = %q (err %v), want %q", location, code, err, diag.CodeLocationUnsupported)
		}
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("ran %+v, want nothing", got)
	}
}

func TestIosDriver_SetLocation_RejectsAPositionOutOfRangeBeforeTheHost(t *testing.T) {
	for _, location := range []driver.Location{{Latitude: 90.5}, {Longitude: -181}} {
		runner := process.NewFake()
		d := newLocationSimulator(runner)
		err := d.SetLocation(context.Background(), location)
		if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
			t.Fatalf("SetLocation(%+v) code = %q (err %v), want %q", location, code, err, diag.CodeInvalidArgument)
		}
		if got := runner.Invocations(); len(got) != 0 {
			t.Fatalf("ran %+v, want nothing", got)
		}
	}
}

func TestIosDriver_SetLocation_ASimctlFailureIsLocationNotSet(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "Unable to set location\nInvalid device state\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newLocationSimulator(runner)

	err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2})
	if code, _, _ := diag.Describe(err); code != diag.CodeLocationNotSet {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeLocationNotSet)
	}
}

func TestIosDriver_SetLocation_ACancelledContextStopsBeforeTheHost(t *testing.T) {
	runner := process.NewFake()
	d := newLocationSimulator(runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.SetLocation(ctx, driver.Location{Latitude: 1, Longitude: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetLocation() error = %v, want context.Canceled", err)
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("ran %+v, want nothing", got)
	}
}
