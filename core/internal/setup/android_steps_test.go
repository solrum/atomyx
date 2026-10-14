package setup

import (
	"context"
	"strings"
	"testing"
)

type fakeLocator map[string]string

func (f fakeLocator) Locate(name string) (string, bool) {
	path, ok := f[name]
	return path, ok
}

type fakeStates map[string]DeviceState

func (f fakeStates) DeviceState(_ context.Context, serial string) DeviceState {
	if state, ok := f[serial]; ok {
		return state
	}
	return DeviceAbsent
}

type fakeAPK struct {
	path  string
	found bool
}

func (f fakeAPK) ResolveAgentAPK() (string, bool) { return f.path, f.found }

func TestAndroidAdbStep(t *testing.T) {
	ok := NewAndroidAdbStep(fakeLocator{"adb": "/opt/sdk/adb"}).Check(context.Background())
	if ok.Status != StatusOK || !strings.Contains(ok.Message, "/opt/sdk/adb") {
		t.Errorf("present: %+v", ok)
	}
	missing := NewAndroidAdbStep(fakeLocator{}).Check(context.Background())
	if missing.Status != StatusManual || !strings.Contains(missing.Remedy, "platform tools") {
		t.Errorf("missing: %+v", missing)
	}
}

func TestAndroidDeviceStep(t *testing.T) {
	states := fakeStates{"on": DeviceOnline, "unauth": DeviceUnauthorized, "off": DeviceOffline, "boot": DeviceUnavailable}
	cases := []struct {
		name, serial string
		status       Status
		remedy       string
	}{
		{"online", "on", StatusOK, ""},
		{"unauthorized", "unauth", StatusManual, "USB debugging prompt"},
		{"offline", "off", StatusManual, "adb kill-server"},
		{"bootloader", "boot", StatusManual, "Boot the device into Android"},
		{"absent", "gone", StatusManual, "atomyx devices"},
		{"unresolved", "", StatusManual, "--device <serial>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAndroidDeviceStep(tc.serial, states).Check(context.Background())
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s: %+v", got.Status, tc.status, got)
			}
			if !strings.Contains(got.Remedy, tc.remedy) {
				t.Errorf("remedy = %q, want it to contain %q", got.Remedy, tc.remedy)
			}
			if tc.status == StatusManual && got.Remedy == "" {
				t.Error("a manual result needs a remedy")
			}
		})
	}
}

func TestAndroidAPKStep(t *testing.T) {
	ok := NewAndroidAPKStep(fakeAPK{path: "/repo/app-debug.apk", found: true}).Check(context.Background())
	if ok.Status != StatusOK || !strings.Contains(ok.Message, "/repo/app-debug.apk") {
		t.Errorf("found: %+v", ok)
	}
	missing := NewAndroidAPKStep(fakeAPK{path: "/repo/app-debug.apk"}).Check(context.Background())
	if missing.Status != StatusManual || !strings.Contains(missing.Remedy, "assembleDebug") || !strings.Contains(missing.Remedy, "ATOMYX_ANDROID_APK") {
		t.Errorf("missing: %+v", missing)
	}
}
