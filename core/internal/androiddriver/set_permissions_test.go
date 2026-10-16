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

const permissionsTestApp = "dev.atomyx.demo"

func newPermissionsDriver(runner *process.Fake) *androiddriver.AndroidDriver {
	return androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})
}

func stubSDK(runner *process.Fake, sdk string) {
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "getprop", "ro.build.version.sdk"),
		process.Result{Stdout: sdk + "\n"}, nil)
}

func pmInvocation(verb, permission string) process.Invocation {
	return process.Invocation{Name: "adb", Args: []string{"-s", "S1", "shell", "pm", verb, "'" + permissionsTestApp + "'", "'" + permission + "'"}}
}

func sdkInvocation() process.Invocation {
	return process.Invocation{Name: "adb", Args: []string{"-s", "S1", "shell", "getprop", "ro.build.version.sdk"}}
}

func TestAndroidDriver_SetPermissions_GrantsAndRevokesEveryMemberOfAGroup(t *testing.T) {
	runner := process.NewFake()
	stubSDK(runner, "34")
	d := newPermissionsDriver(runner)

	err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{
		driver.PermissionCamera:   driver.PermissionAllow,
		driver.PermissionLocation: driver.PermissionDeny,
	})
	if err != nil {
		t.Fatalf("SetPermissions() error = %v", err)
	}
	want := []process.Invocation{
		sdkInvocation(),
		pmInvocation("grant", "android.permission.CAMERA"),
		pmInvocation("revoke", "android.permission.ACCESS_FINE_LOCATION"),
		pmInvocation("revoke", "android.permission.ACCESS_COARSE_LOCATION"),
	}
	if got := runner.Invocations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("invocations = %+v, want %+v", got, want)
	}
}

func TestAndroidDriver_SetPermissions_PicksTheMappingForTheApiLevel(t *testing.T) {
	tests := []struct {
		sdk        string
		permission driver.Permission
		want       []string
	}{
		{"34", driver.PermissionPhotos, []string{"android.permission.READ_MEDIA_IMAGES", "android.permission.READ_MEDIA_VIDEO"}},
		{"31", driver.PermissionPhotos, []string{"android.permission.READ_EXTERNAL_STORAGE"}},
		{"33", driver.PermissionNotifications, []string{"android.permission.POST_NOTIFICATIONS"}},
		{"23", driver.PermissionMicrophone, []string{"android.permission.RECORD_AUDIO"}},
	}
	for _, tc := range tests {
		t.Run(string(tc.permission)+"-"+tc.sdk, func(t *testing.T) {
			runner := process.NewFake()
			stubSDK(runner, tc.sdk)
			d := newPermissionsDriver(runner)
			err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{tc.permission: driver.PermissionAllow})
			if err != nil {
				t.Fatalf("SetPermissions() error = %v", err)
			}
			want := []process.Invocation{sdkInvocation()}
			for _, p := range tc.want {
				want = append(want, pmInvocation("grant", p))
			}
			if got := runner.Invocations(); !reflect.DeepEqual(got, want) {
				t.Fatalf("invocations = %+v, want %+v", got, want)
			}
		})
	}
}

func TestAndroidDriver_SetPermissions_AnUnmappedNameIsUnsupportedAndChangesNothing(t *testing.T) {
	tests := []struct {
		name string
		sdk  string
		perm driver.Permission
	}{
		{"notifications before they were runtime permissions", "32", driver.PermissionNotifications},
		{"any permission before runtime permissions", "22", driver.PermissionCamera},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			stubSDK(runner, tc.sdk)
			d := newPermissionsDriver(runner)
			err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{
				driver.PermissionContacts: driver.PermissionAllow,
				tc.perm:                   driver.PermissionAllow,
			})
			code, message, _ := diag.Describe(err)
			if code != diag.CodePermissionsUnsupported {
				t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodePermissionsUnsupported)
			}
			if !strings.Contains(message, string(tc.perm)) && tc.perm != driver.PermissionCamera {
				t.Fatalf("message %q does not name %s", message, tc.perm)
			}
			if got := runner.Invocations(); len(got) != 1 {
				t.Fatalf("invocations = %+v, want only the level read", got)
			}
		})
	}
}

func TestAndroidDriver_SetPermissions_SkipsAMemberTheAppDoesNotDeclare(t *testing.T) {
	runner := process.NewFake()
	stubSDK(runner, "34")
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "pm", "grant", "'"+permissionsTestApp+"'", "'android.permission.WRITE_CONTACTS'"),
		process.Result{Stderr: "java.lang.SecurityException: Package dev.atomyx.demo has not requested permission android.permission.WRITE_CONTACTS\n", ExitCode: 255},
		errors.New("exit status 255"))
	d := newPermissionsDriver(runner)

	err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{driver.PermissionContacts: driver.PermissionAllow})
	if err != nil {
		t.Fatalf("SetPermissions() error = %v, want nil because READ_CONTACTS was granted", err)
	}
}

func TestAndroidDriver_SetPermissions_FailsWhenTheAppDeclaresNoneOfTheGroup(t *testing.T) {
	runner := process.NewFake()
	stubSDK(runner, "34")
	runner.Default = process.Result{Stderr: "java.lang.SecurityException: Package x has not requested permission y\n", ExitCode: 255}
	runner.DefaultErr = errors.New("exit status 255")
	d := newPermissionsDriver(runner)

	err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	if code, _, _ := diag.Describe(err); code != diag.CodePermissionNotSet {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodePermissionNotSet)
	}
}

func TestAndroidDriver_SetPermissions_AnyOtherRefusalIsPermissionNotSet(t *testing.T) {
	runner := process.NewFake()
	stubSDK(runner, "34")
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "pm", "grant", "'"+permissionsTestApp+"'", "'android.permission.CAMERA'"),
		process.Result{Stderr: "java.lang.IllegalArgumentException: Unknown package: dev.atomyx.demo\n", ExitCode: 255},
		errors.New("exit status 255"))
	d := newPermissionsDriver(runner)

	err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	code, message, _ := diag.Describe(err)
	if code != diag.CodePermissionNotSet || !strings.Contains(message, "Unknown package") {
		t.Fatalf("code = %q message %q, want %q naming the device's reason", code, message, diag.CodePermissionNotSet)
	}
}

func TestAndroidDriver_SetPermissions_RejectsBadInputBeforeTheShell(t *testing.T) {
	tests := []struct {
		name        string
		appID       string
		permissions map[driver.Permission]driver.PermissionState
	}{
		{"empty map", permissionsTestApp, nil},
		{"unknown name", permissionsTestApp, map[driver.Permission]driver.PermissionState{"bluetooth": driver.PermissionAllow}},
		{"unknown state", permissionsTestApp, map[driver.Permission]driver.PermissionState{driver.PermissionCamera: "maybe"}},
		{"bad package", "com.x; reboot", map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			d := newPermissionsDriver(runner)
			err := d.SetPermissions(context.Background(), tc.appID, tc.permissions)
			if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
				t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
			}
			if got := runner.Invocations(); len(got) != 0 {
				t.Fatalf("ran %+v, want no shell call", got)
			}
		})
	}
}

func TestAndroidDriver_SetPermissions_UnreachableDeviceIsReportedAsUnreachable(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "error: device offline\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newPermissionsDriver(runner)

	err := d.SetPermissions(context.Background(), permissionsTestApp, map[driver.Permission]driver.PermissionState{driver.PermissionCamera: driver.PermissionAllow})
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeAgentUnreachable)
	}
}

func TestAndroidDriver_SetPermissions_ReportsTheCapability(t *testing.T) {
	if !newPermissionsDriver(process.NewFake()).Capabilities().CanSetPermissions {
		t.Fatal("Capabilities().CanSetPermissions = false, want true")
	}
}
