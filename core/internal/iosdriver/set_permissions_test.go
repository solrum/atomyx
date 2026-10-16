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

func newPermissionsSimulator(runner *process.Fake) *iosdriver.IosDriver {
	return iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
}

func TestIosDriver_SetPermissions_SimulatorSetsEachPrivacyService(t *testing.T) {
	runner := process.NewFake()
	d := newPermissionsSimulator(runner)

	err := d.SetPermissions(context.Background(), "dev.atomyx.demo", map[driver.Permission]driver.PermissionState{
		driver.PermissionLocation: driver.PermissionAllow,
		driver.PermissionPhotos:   driver.PermissionDeny,
	})
	if err != nil {
		t.Fatalf("SetPermissions() error = %v", err)
	}
	want := []process.Invocation{
		{Name: "xcrun", Args: []string{"simctl", "privacy", "SIM-1", "grant", "location", "dev.atomyx.demo"}},
		{Name: "xcrun", Args: []string{"simctl", "privacy", "SIM-1", "revoke", "photos", "dev.atomyx.demo"}},
	}
	if got := runner.Invocations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("invocations = %+v, want %+v", got, want)
	}
}

func TestIosDriver_SetPermissions_MapsEveryServiceTheSimulatorOffers(t *testing.T) {
	want := map[driver.Permission]string{
		driver.PermissionLocation:   "location",
		driver.PermissionMicrophone: "microphone",
		driver.PermissionPhotos:     "photos",
		driver.PermissionContacts:   "contacts",
		driver.PermissionCalendar:   "calendar",
	}
	for name, service := range want {
		runner := process.NewFake()
		d := newPermissionsSimulator(runner)
		if err := d.SetPermissions(context.Background(), "dev.atomyx.demo", map[driver.Permission]driver.PermissionState{name: driver.PermissionAllow}); err != nil {
			t.Fatalf("SetPermissions(%s) error = %v", name, err)
		}
		got := runner.Invocations()
		if len(got) != 1 || got[0].Args[4] != service {
			t.Fatalf("SetPermissions(%s) invocations = %+v, want service %q", name, got, service)
		}
	}
}

func TestIosDriver_SetPermissions_ANameWithoutAServiceIsUnsupportedAndChangesNothing(t *testing.T) {
	for _, name := range []driver.Permission{driver.PermissionCamera, driver.PermissionNotifications} {
		runner := process.NewFake()
		d := newPermissionsSimulator(runner)
		err := d.SetPermissions(context.Background(), "dev.atomyx.demo", map[driver.Permission]driver.PermissionState{
			driver.PermissionLocation: driver.PermissionAllow,
			name:                      driver.PermissionAllow,
		})
		if code, _, _ := diag.Describe(err); code != diag.CodePermissionsUnsupported {
			t.Fatalf("SetPermissions(%s) code = %q (err %v), want %q", name, code, err, diag.CodePermissionsUnsupported)
		}
		if got := runner.Invocations(); len(got) != 0 {
			t.Fatalf("SetPermissions(%s) ran %+v, want nothing", name, got)
		}
	}
}

func TestIosDriver_SetPermissions_PhysicalDeviceIsUnsupported(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	if d.Capabilities().CanSetPermissions {
		t.Fatal("Capabilities().CanSetPermissions = true on a device, want false")
	}
	err := d.SetPermissions(context.Background(), "dev.atomyx.demo", map[driver.Permission]driver.PermissionState{driver.PermissionLocation: driver.PermissionAllow})
	if code, _, _ := diag.Describe(err); code != diag.CodePermissionsUnsupported {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodePermissionsUnsupported)
	}
	if got := runner.Invocations(); len(got) != 0 {
		t.Fatalf("ran %+v, want nothing", got)
	}
}

func TestIosDriver_SetPermissions_SimulatorReportsTheCapability(t *testing.T) {
	if !newPermissionsSimulator(process.NewFake()).Capabilities().CanSetPermissions {
		t.Fatal("Capabilities().CanSetPermissions = false on a simulator, want true")
	}
}

func TestIosDriver_SetPermissions_RejectsBadInputBeforeTheHost(t *testing.T) {
	tests := []struct {
		name        string
		appID       string
		permissions map[driver.Permission]driver.PermissionState
	}{
		{"empty map", "dev.atomyx.demo", nil},
		{"unknown name", "dev.atomyx.demo", map[driver.Permission]driver.PermissionState{"bluetooth": driver.PermissionAllow}},
		{"empty app id", "", map[driver.Permission]driver.PermissionState{driver.PermissionPhotos: driver.PermissionAllow}},
		{"option-like app id", "--all", map[driver.Permission]driver.PermissionState{driver.PermissionPhotos: driver.PermissionAllow}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			d := newPermissionsSimulator(runner)
			err := d.SetPermissions(context.Background(), tc.appID, tc.permissions)
			if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
				t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
			}
			if got := runner.Invocations(); len(got) != 0 {
				t.Fatalf("ran %+v, want nothing", got)
			}
		})
	}
}

func TestIosDriver_SetPermissions_ASimctlFailureIsPermissionNotSet(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "An error was encountered processing the command\nInvalid bundle identifier\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newPermissionsSimulator(runner)

	err := d.SetPermissions(context.Background(), "dev.atomyx.demo", map[driver.Permission]driver.PermissionState{driver.PermissionPhotos: driver.PermissionAllow})
	if code, _, _ := diag.Describe(err); code != diag.CodePermissionNotSet {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodePermissionNotSet)
	}
}
