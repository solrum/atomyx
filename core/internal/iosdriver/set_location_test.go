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

func newLocationDevice(conn *fakeConn, runner *process.Fake) *iosdriver.IosDriver {
	return iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})
}

func TestIosDriver_SetLocation_PhysicalDeviceCallsTheRunner(t *testing.T) {
	conn := newDefaultFakeConn()
	runner := process.NewFake()
	d := newLocationDevice(conn, runner)

	if !d.Capabilities().CanSetLocation {
		t.Fatal("Capabilities().CanSetLocation = false on a device, want true")
	}
	if err := d.SetLocation(context.Background(), driver.Location{Latitude: 10.7769, Longitude: 106.7009}); err != nil {
		t.Fatalf("SetLocation() error = %v", err)
	}
	calls := conn.Calls()
	if len(calls) != 1 || calls[0].cmdType != "setLocation" {
		t.Fatalf("calls = %+v, want one setLocation", calls)
	}
	want := map[string]any{"latitude": 10.7769, "longitude": 106.7009}
	if !reflect.DeepEqual(calls[0].args, want) {
		t.Fatalf("setLocation args = %#v, want %#v", calls[0].args, want)
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("ran %+v, want nothing on the host", got)
	}
}

func TestIosDriver_SetLocation_SimulatorDoesNotCallTheRunner(t *testing.T) {
	conn := newDefaultFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2}); err != nil {
		t.Fatalf("SetLocation() error = %v", err)
	}
	if calls := conn.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %+v, want none", calls)
	}
}

func TestIosDriver_SetLocation_PhysicalDeviceOldOsIsUnsupported(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("setLocation", func(map[string]any) (map[string]any, error) {
		return map[string]any{"set": false, "reason": "unsupported-os"}, nil
	})
	d := newLocationDevice(conn, process.NewFake())

	err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2})
	if code, _, _ := diag.Describe(err); code != diag.CodeLocationUnsupported {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeLocationUnsupported)
	}
}

func TestIosDriver_SetLocation_PhysicalDeviceRefusalIsLocationNotSet(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("setLocation", func(map[string]any) (map[string]any, error) {
		return map[string]any{"set": false}, nil
	})
	d := newLocationDevice(conn, process.NewFake())

	err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2})
	if code, _, _ := diag.Describe(err); code != diag.CodeLocationNotSet {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeLocationNotSet)
	}
}

func TestIosDriver_SetLocation_PhysicalDeviceRejectsAnInvalidPositionBeforeTheRunner(t *testing.T) {
	conn := newDefaultFakeConn()
	d := newLocationDevice(conn, process.NewFake())

	err := d.SetLocation(context.Background(), driver.Location{Latitude: 99})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if calls := conn.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %+v, want none", calls)
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
