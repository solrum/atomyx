package androiddriver_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
)

func newLocationDriver(serial string, runner *process.Fake) *androiddriver.AndroidDriver {
	return androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: serial})
}

func TestAndroidDriver_SetLocation_EmulatorSendsLongitudeBeforeLatitude(t *testing.T) {
	tests := []struct {
		name     string
		location driver.Location
		lon, lat string
	}{
		{"northern and western", driver.Location{Latitude: 37.7749, Longitude: -122.4194}, "-122.4194", "37.7749"},
		{"southern and eastern", driver.Location{Latitude: -33.8568, Longitude: 151.2153}, "151.2153", "-33.8568"},
		{"whole numbers", driver.Location{Latitude: 10, Longitude: 20}, "20", "10"},
		{"tiny values stay decimal", driver.Location{Latitude: 0.0000001, Longitude: 0}, "0", "0.0000001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			runner.Default = process.Result{Stdout: "OK\n"}
			d := newLocationDriver("emulator-5554", runner)

			if err := d.SetLocation(context.Background(), tc.location); err != nil {
				t.Fatalf("SetLocation() error = %v", err)
			}
			want := []process.Invocation{{Name: "adb", Args: []string{"-s", "emulator-5554", "emu", "geo", "fix", tc.lon, tc.lat}}}
			if got := runner.Invocations(); !reflect.DeepEqual(got, want) {
				t.Fatalf("invocations = %+v, want %+v", got, want)
			}
		})
	}
}

func TestAndroidDriver_SetLocation_CapabilityFollowsTheSerial(t *testing.T) {
	tests := map[string]bool{
		"emulator-5554":    true,
		"emulator-5556":    true,
		"R58M123ABC":       false,
		"192.168.1.5:5555": false,
	}
	for serial, want := range tests {
		if got := newLocationDriver(serial, process.NewFake()).Capabilities().CanSetLocation; got != want {
			t.Errorf("serial %q: CanSetLocation = %v, want %v", serial, got, want)
		}
	}
}

func TestAndroidDriver_SetLocation_PhysicalDeviceIsUnsupported(t *testing.T) {
	runner := process.NewFake()
	d := newLocationDriver("R58M123ABC", runner)

	for _, location := range []driver.Location{{Latitude: 1, Longitude: 2}, {Latitude: 99}} {
		err := d.SetLocation(context.Background(), location)
		code, message, _ := diag.Describe(err)
		if code != diag.CodeLocationUnsupported {
			t.Fatalf("SetLocation(%+v) code = %q (err %v), want %q", location, code, err, diag.CodeLocationUnsupported)
		}
		if want := "mock location provider"; !strings.Contains(message, want) {
			t.Fatalf("message %q does not mention %q", message, want)
		}
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("ran %+v, want nothing", got)
	}
}

func TestAndroidDriver_SetLocation_RejectsAPositionOutOfRangeBeforeTheShell(t *testing.T) {
	for _, location := range []driver.Location{{Latitude: -90.1}, {Longitude: 180.1}} {
		runner := process.NewFake()
		d := newLocationDriver("emulator-5554", runner)
		err := d.SetLocation(context.Background(), location)
		if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
			t.Fatalf("SetLocation(%+v) code = %q (err %v), want %q", location, code, err, diag.CodeInvalidArgument)
		}
		if got := runner.Invocations(); len(got) != 0 {
			t.Fatalf("ran %+v, want nothing", got)
		}
	}
}

func TestAndroidDriver_SetLocation_AConsoleRefusalIsLocationNotSet(t *testing.T) {
	for _, output := range []string{"KO: invalid longitude\n", "KO\n", "some banner\nKO: bad\n"} {
		runner := process.NewFake()
		runner.Default = process.Result{Stdout: output}
		d := newLocationDriver("emulator-5554", runner)

		err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2})
		if code, _, _ := diag.Describe(err); code != diag.CodeLocationNotSet {
			t.Fatalf("output %q: code = %q (err %v), want %q", output, code, err, diag.CodeLocationNotSet)
		}
	}
}

func TestAndroidDriver_SetLocation_AFailedCommandIsLocationNotSet(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "adb: failed to connect to the console\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newLocationDriver("emulator-5554", runner)

	err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2})
	if code, _, _ := diag.Describe(err); code != diag.CodeLocationNotSet {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeLocationNotSet)
	}
}

func TestAndroidDriver_SetLocation_UnreachableDeviceIsReportedAsUnreachable(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "error: device offline\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newLocationDriver("emulator-5554", runner)

	err := d.SetLocation(context.Background(), driver.Location{Latitude: 1, Longitude: 2})
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeAgentUnreachable)
	}
}

func TestAndroidDriver_SetLocation_ACancelledContextStopsBeforeTheShell(t *testing.T) {
	runner := process.NewFake()
	d := newLocationDriver("emulator-5554", runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.SetLocation(ctx, driver.Location{Latitude: 1, Longitude: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetLocation() error = %v, want context.Canceled", err)
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("ran %+v, want nothing", got)
	}
}
