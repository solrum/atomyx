package device

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/process"
)

const simctlJSON = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-17-4": [
      {"udid": "SIM-BOOTED", "name": "iPhone 15", "state": "Booted", "isAvailable": true},
      {"udid": "SIM-SHUTDOWN", "name": "iPhone SE", "state": "Shutdown", "isAvailable": true},
      {"udid": "SIM-UNAVAILABLE", "name": "iPad Air", "state": "Shutdown", "isAvailable": false}
    ]
  }
}`

func TestIosProbe_Scan_SimulatorsFilteredToReachableAndAvailable(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "--json"), process.Result{Stdout: simctlJSON}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: ""}, nil)

	got := NewIosProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "SIM-BOOTED", Platform: PlatformIOS, Name: "iPhone 15 · iOS-17-4", Kind: KindSimulator, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestIosProbe_Scan_PhysicalDevice_ResolvesNameViaIdevicename(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "--json"), process.Result{Stdout: `{"devices":{}}`}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: "00008030-ABCDEF\n"}, nil)
	fake.Stub(process.ExactMatch("idevicename", "-u", "00008030-ABCDEF"), process.Result{Stdout: "Hieu's iPhone\n"}, nil)

	got := NewIosProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "00008030-ABCDEF", Platform: PlatformIOS, Name: "Hieu's iPhone", Kind: KindDevice, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestIosProbe_Scan_IdevicenameFails_FallsBackToUDID(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "--json"), process.Result{Stdout: `{"devices":{}}`}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: "00008030-ABCDEF\n"}, nil)
	fake.Stub(process.ExactMatch("idevicename", "-u", "00008030-ABCDEF"), process.Result{}, errors.New("no such device"))

	got := NewIosProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "00008030-ABCDEF", Platform: PlatformIOS, Name: "00008030-ABCDEF", Kind: KindDevice, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestIosProbe_Scan_XcrunMissing_StillReturnsPhysicalDevices(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "--json"), process.Result{}, errors.New("xcrun: command not found"))
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: "AAA111\n"}, nil)
	fake.Stub(process.ExactMatch("idevicename", "-u", "AAA111"), process.Result{Stdout: "Some iPhone"}, nil)

	got := NewIosProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "AAA111", Platform: PlatformIOS, Name: "Some iPhone", Kind: KindDevice, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestIosProbe_Scan_IdeviceIdMissing_StillReturnsSimulators(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "--json"), process.Result{Stdout: simctlJSON}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{}, errors.New("idevice_id: command not found"))

	got := NewIosProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "SIM-BOOTED", Platform: PlatformIOS, Name: "iPhone 15 · iOS-17-4", Kind: KindSimulator, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestIosBootedSimulatorProbe_Scan_ReportsEveryBootedEntryUnfiltered(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "booted", "--json"), process.Result{Stdout: `{
		"devices": {
			"com.apple.CoreSimulator.SimRuntime.iOS-17-4": [
				{"udid": "SIM-BOOTED", "name": "iPhone 15", "state": "Booted", "isAvailable": false}
			]
		}
	}`}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: ""}, nil)

	got := NewIosBootedSimulatorProbe(fake).Scan(context.Background())
	// No runtime-label suffix and no availability filter, unlike
	// IosProbe: isAvailable is false above yet the entry is still
	// reported, matching the TypeScript CLI's own booted-only scan.
	want := []Descriptor{
		{ID: "SIM-BOOTED", Platform: PlatformIOS, Name: "iPhone 15", Kind: KindSimulator, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestIosBootedSimulatorProbe_Scan_OrdersRuntimesDeterministically(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "booted", "--json"), process.Result{Stdout: `{
		"devices": {
			"com.apple.CoreSimulator.SimRuntime.iOS-18-0": [
				{"udid": "SIM-B", "name": "iPad", "state": "Booted", "isAvailable": true}
			],
			"com.apple.CoreSimulator.SimRuntime.iOS-17-4": [
				{"udid": "SIM-A", "name": "iPhone 15", "state": "Booted", "isAvailable": true}
			]
		}
	}`}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: ""}, nil)

	got := NewIosBootedSimulatorProbe(fake).Scan(context.Background())
	if len(got) != 2 || got[0].ID != "SIM-A" || got[1].ID != "SIM-B" {
		t.Fatalf("Scan() = %+v, want SIM-A (iOS-17-4) before SIM-B (iOS-18-0)", got)
	}
}

func TestIosBootedSimulatorProbe_Scan_IncludesPhysicalDevices(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("xcrun", "simctl", "list", "devices", "booted", "--json"), process.Result{Stdout: `{"devices":{}}`}, nil)
	fake.Stub(process.ExactMatch("idevice_id", "-l"), process.Result{Stdout: "00008030-ABCDEF\n"}, nil)
	fake.Stub(process.ExactMatch("idevicename", "-u", "00008030-ABCDEF"), process.Result{Stdout: "Hieu's iPhone\n"}, nil)

	got := NewIosBootedSimulatorProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "00008030-ABCDEF", Platform: PlatformIOS, Name: "Hieu's iPhone", Kind: KindDevice, State: StateOnline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestSortBootedFirst_BootedBeforeOthersThenAlphabetical(t *testing.T) {
	entries := []iosDeviceEntry{
		{udid: "b", name: "Zebra", state: "Shutdown"},
		{udid: "c", name: "Apple", state: "Shutdown"},
		{udid: "a", name: "Mango", state: "Booted"},
	}
	got := sortBootedFirst(entries)
	wantOrder := []string{"a", "c", "b"}
	for i, e := range got {
		if e.udid != wantOrder[i] {
			t.Fatalf("sortBootedFirst() order = %v, want %v", udids(got), wantOrder)
		}
	}
}

func udids(entries []iosDeviceEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.udid
	}
	return out
}
