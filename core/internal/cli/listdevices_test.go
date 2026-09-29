package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
)

func TestListDevicesTextGroupsByPlatformAndKind(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{
		{ID: "emulator-5554", Platform: device.PlatformAndroid, Name: "emulator-5554", Kind: device.KindEmulator, State: device.StateOnline},
		{ID: "R58N90ABCDE", Platform: device.PlatformAndroid, Name: "Pixel 7", Kind: device.KindDevice, State: device.StateOnline},
		{ID: "OFFLINE-SERIAL", Platform: device.PlatformAndroid, Name: "OFFLINE-SERIAL", Kind: device.KindDevice, State: device.StateOffline},
		{ID: "UNAUTH-SERIAL", Platform: device.PlatformAndroid, Name: "UNAUTH-SERIAL", Kind: device.KindDevice, State: device.StateUnauthorized},
		{ID: "SIM-UDID", Platform: device.PlatformIOS, Name: "iPhone 15", Kind: device.KindSimulator, State: device.StateOnline},
		{ID: "PHYS-UDID", Platform: device.PlatformIOS, Name: "Hieu's iPhone", Kind: device.KindDevice, State: device.StateOnline},
	})

	var buf bytes.Buffer
	code := ListDevices(context.Background(), probe, false, &buf)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	out := buf.String()

	// State literals below match the TypeScript CLI's own rendering,
	// not the internal device.State enum: a reachable Android device
	// reads "ready", an unreachable one passes its raw adb state
	// through unchanged, a booted iOS simulator reads "booted", and a
	// connected physical iOS device reads "connected".
	for _, want := range []string{
		"Atomyx: connected devices",
		"Android (via adb):",
		"  emulator-5554  [ready]\n",
		"  R58N90ABCDE  [ready] Pixel 7\n",
		"  OFFLINE-SERIAL  [offline]\n",
		"  UNAUTH-SERIAL  [unauthorized]\n",
		"iOS Simulators (booted):",
		"  SIM-UDID  [booted] iPhone 15\n",
		"iOS Devices (USB):",
		"  PHYS-UDID  [connected] Hieu's iPhone\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

func TestListDevicesTextEmptyGroupsShowNone(t *testing.T) {
	probe := device.NewStaticProbe(nil)
	var buf bytes.Buffer
	ListDevices(context.Background(), probe, false, &buf)
	out := buf.String()
	if strings.Count(out, "(none)") != 3 {
		t.Errorf("expected 3 empty groups, got:\n%s", out)
	}
}

func TestDisplayStateMatchesTypeScriptCliVocabulary(t *testing.T) {
	cases := []struct {
		name string
		d    device.Descriptor
		want string
	}{
		{"android reachable", device.Descriptor{Platform: device.PlatformAndroid, Kind: device.KindDevice, State: device.StateOnline}, "ready"},
		{"android offline", device.Descriptor{Platform: device.PlatformAndroid, Kind: device.KindDevice, State: device.StateOffline}, "offline"},
		{"android unauthorized", device.Descriptor{Platform: device.PlatformAndroid, Kind: device.KindDevice, State: device.StateUnauthorized}, "unauthorized"},
		{"android emulator reachable", device.Descriptor{Platform: device.PlatformAndroid, Kind: device.KindEmulator, State: device.StateOnline}, "ready"},
		{"ios simulator", device.Descriptor{Platform: device.PlatformIOS, Kind: device.KindSimulator, State: device.StateOnline}, "booted"},
		{"ios physical device", device.Descriptor{Platform: device.PlatformIOS, Kind: device.KindDevice, State: device.StateOnline}, "connected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayState(tc.d); got != tc.want {
				t.Errorf("displayState(%+v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

func TestListDevicesJSONEnvelope(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{
		{ID: "emulator-5554", Platform: device.PlatformAndroid, Name: "emulator-5554", Kind: device.KindEmulator, State: device.StateOnline},
	})
	var buf bytes.Buffer
	code := ListDevices(context.Background(), probe, true, &buf)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var envelope devicesEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(envelope.Devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(envelope.Devices))
	}
	got := envelope.Devices[0]
	want := deviceEntry{Platform: "android", ID: "emulator-5554", Name: "emulator-5554", State: "ready", Kind: "emulator"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if envelope.Warnings == nil || len(envelope.Warnings) != 0 {
		t.Errorf("warnings: got %#v, want empty array", envelope.Warnings)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("output does not end with a trailing newline")
	}
	if !strings.Contains(buf.String(), "\": \"") {
		t.Errorf("output is not space-after-colon indented JSON:\n%s", buf.String())
	}
}
