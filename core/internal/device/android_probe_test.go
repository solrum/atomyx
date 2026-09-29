package device

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/process"
)

func TestAndroidAdbProbe_Scan_ParsesEmulatorAndDeviceAndUnauthorized(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "devices", "-l"), process.Result{
		Stdout: "List of devices attached\n" +
			"emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64\n" +
			"R58N90ABCDE            device usb:1-1 product:starlte\n" +
			"R58N90XXXXX            unauthorized usb:1-2\n",
	}, nil)

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "emulator-5554", Platform: PlatformAndroid, Name: "emulator-5554", Kind: KindEmulator, State: StateOnline},
		{ID: "R58N90ABCDE", Platform: PlatformAndroid, Name: "R58N90ABCDE", Kind: KindDevice, State: StateOnline},
		{ID: "R58N90XXXXX", Platform: PlatformAndroid, Name: "R58N90XXXXX", Kind: KindDevice, State: StateUnauthorized},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestAndroidAdbProbe_Scan_OfflineDevice_MapsToStateOffline(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "devices", "-l"), process.Result{
		Stdout: "List of devices attached\nR58N90ABCDE            offline\n",
	}, nil)

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "R58N90ABCDE", Platform: PlatformAndroid, Name: "R58N90ABCDE", Kind: KindDevice, State: StateOffline},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestAndroidAdbProbe_Scan_BootloaderRecoverySideload_MapToStateUnavailable(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "devices", "-l"), process.Result{
		Stdout: "List of devices attached\n" +
			"R58N90AAAAA            bootloader\n" +
			"R58N90BBBBB            recovery\n" +
			"R58N90CCCCC            sideload\n",
	}, nil)

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "R58N90AAAAA", Platform: PlatformAndroid, Name: "R58N90AAAAA", Kind: KindDevice, State: StateUnavailable},
		{ID: "R58N90BBBBB", Platform: PlatformAndroid, Name: "R58N90BBBBB", Kind: KindDevice, State: StateUnavailable},
		{ID: "R58N90CCCCC", Platform: PlatformAndroid, Name: "R58N90CCCCC", Kind: KindDevice, State: StateUnavailable},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestAndroidAdbProbe_Scan_UnrecognizedState_MapsToStateUnauthorized(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "devices", "-l"), process.Result{
		Stdout: "List of devices attached\nR58N90ABCDE            no-permissions\n",
	}, nil)

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	want := []Descriptor{
		{ID: "R58N90ABCDE", Platform: PlatformAndroid, Name: "R58N90ABCDE", Kind: KindDevice, State: StateUnauthorized},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %+v, want %+v", got, want)
	}
}

func TestAndroidAdbProbe_Scan_HeaderOnly_ReturnsEmpty(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "devices", "-l"), process.Result{
		Stdout: "List of devices attached\n",
	}, nil)

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	if len(got) != 0 {
		t.Fatalf("Scan() = %+v, want empty", got)
	}
}

func TestAndroidAdbProbe_Scan_EmptyOutput_ReturnsEmpty(t *testing.T) {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "devices", "-l"), process.Result{Stdout: ""}, nil)

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	if len(got) != 0 {
		t.Fatalf("Scan() = %+v, want empty", got)
	}
}

func TestAndroidAdbProbe_Scan_AdbMissing_ReturnsEmptyNotError(t *testing.T) {
	fake := process.NewFake()
	fake.DefaultErr = errors.New("adb not found on PATH")

	got := NewAndroidAdbProbe(fake).Scan(context.Background())
	if got != nil {
		t.Fatalf("Scan() = %+v, want nil when adb is missing", got)
	}
}
