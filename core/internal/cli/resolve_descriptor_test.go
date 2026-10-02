package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
)

const (
	testSimulatorUDID = "SIM-UDID-1"
	testPhysicalUDID  = "00008101-001529640E52001E"
)

func iosDevicesProbe() device.Probe {
	return device.NewStaticProbe([]device.Descriptor{
		{ID: testSimulatorUDID, Platform: device.PlatformIOS, Name: "iPhone 15", Kind: device.KindSimulator, State: device.StateOnline},
		{ID: testPhysicalUDID, Platform: device.PlatformIOS, Name: "Bulbasaur", Kind: device.KindDevice, State: device.StateOnline},
	})
}

func configEnvDeviceProbe() device.Probe {
	return device.NewStaticProbe([]device.Descriptor{
		{ID: "config-device-1", Platform: device.PlatformIOS, Name: "Config device", Kind: device.KindSimulator, State: device.StateOnline},
	})
}

type recordingFactory struct {
	inner device.Factory
	got   []device.Descriptor
}

func (f *recordingFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	f.got = append(f.got, desc)
	return f.inner.Build(ctx, desc)
}

func TestResolveDescriptorIosSimulatorUdid(t *testing.T) {
	desc, err := resolveDescriptor(context.Background(), iosDevicesProbe(), "ios", testSimulatorUDID)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Kind != device.KindSimulator || desc.ID != testSimulatorUDID {
		t.Fatalf("got %+v, want simulator %s", desc, testSimulatorUDID)
	}
}

func TestResolveDescriptorIosPhysicalUdid(t *testing.T) {
	desc, err := resolveDescriptor(context.Background(), iosDevicesProbe(), "ios", testPhysicalUDID)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Kind != device.KindDevice || desc.ID != testPhysicalUDID {
		t.Fatalf("got %+v, want physical device %s", desc, testPhysicalUDID)
	}
}

func TestResolveDescriptorIosUnknownUdidIsDeviceNotFound(t *testing.T) {
	_, err := resolveDescriptor(context.Background(), iosDevicesProbe(), "ios", "UNKNOWN")
	if err == nil {
		t.Fatal("want an error for an unknown udid")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeDeviceNotFound {
		t.Errorf("code = %q, want %q", code, diag.CodeDeviceNotFound)
	}
	if !strings.Contains(message, "UNKNOWN") {
		t.Errorf("message %q does not name the udid", message)
	}
	if !strings.Contains(remedy, "list-devices") {
		t.Errorf("remedy %q does not point at list-devices", remedy)
	}
}

func TestResolveDescriptorNoDeviceFlagKeepsDefaults(t *testing.T) {
	ios, err := resolveDescriptor(context.Background(), nil, "ios", "")
	if err != nil || ios.ID != "" || ios.Kind != device.KindSimulator {
		t.Fatalf("ios default: got %+v, %v", ios, err)
	}
	android, err := resolveDescriptor(context.Background(), nil, "android", "")
	if err != nil || android.ID != "emulator-5554" || android.Kind != device.KindEmulator {
		t.Fatalf("android default: got %+v, %v", android, err)
	}
}

func TestDoctorBuildsPhysicalDeviceForPhysicalUdid(t *testing.T) {
	var stdout, stderr bytes.Buffer
	factory := &recordingFactory{inner: singleMockDriverFactory{driver: mockdriver.New()}}
	deps := Deps{Probe: iosDevicesProbe(), Factory: factory, Stdout: &stdout, Stderr: &stderr}

	code := Doctor(context.Background(), deps, map[string]FlagValue{"--platform": "ios", "--device": testPhysicalUDID})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if len(factory.got) != 1 || factory.got[0].Kind != device.KindDevice || factory.got[0].ID != testPhysicalUDID {
		t.Fatalf("factory descriptors = %+v, want one physical device %s", factory.got, testPhysicalUDID)
	}
}

func TestDoctorUnknownIosUdidFailsBeforeBuilding(t *testing.T) {
	var stdout, stderr bytes.Buffer
	factory := &recordingFactory{inner: singleMockDriverFactory{driver: mockdriver.New()}}
	deps := Deps{Probe: iosDevicesProbe(), Factory: factory, Stdout: &stdout, Stderr: &stderr}

	code := Doctor(context.Background(), deps, map[string]FlagValue{"--platform": "ios", "--device": "UNKNOWN"})
	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if len(factory.got) != 0 {
		t.Errorf("factory was called for an unresolvable device: %+v", factory.got)
	}
	if !strings.Contains(stderr.String(), diag.CodeDeviceNotFound) {
		t.Errorf("stderr %q lacks %s", stderr.String(), diag.CodeDeviceNotFound)
	}
}
