package device

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/process"
)

func stubProp(f *process.Fake, serial, prop, value string) {
	f.Stub(process.ExactMatch("adb", "-s", serial, "shell", "getprop", prop), process.Result{Stdout: value + "\n"}, nil)
}

func resolve(t *testing.T, f *process.Fake, d Descriptor) (Identity, error) {
	t.Helper()
	return NewIdentityResolver(f).Resolve(context.Background(), d)
}

func androidOnline(id string) Descriptor {
	return Descriptor{ID: id, Platform: PlatformAndroid, State: StateOnline}
}

func TestIdentity_AndroidUsesRoSerialno(t *testing.T) {
	f := process.NewFake()
	stubProp(f, "R58N90", "ro.serialno", "R58N90ABCDE")

	id, err := resolve(t, f, androidOnline("R58N90"))
	if err != nil {
		t.Fatal(err)
	}
	if id != (Identity{Platform: PlatformAndroid, HardwareID: "R58N90ABCDE"}) {
		t.Fatalf("identity = %+v", id)
	}
}

func TestIdentity_WifiAndUsbSerialsResolveToOneIdentity(t *testing.T) {
	f := process.NewFake()
	stubProp(f, "R58N90ABCDE", "ro.serialno", "R58N90ABCDE")
	stubProp(f, "192.168.1.9:5555", "ro.serialno", "R58N90ABCDE")

	usb, err := resolve(t, f, androidOnline("R58N90ABCDE"))
	if err != nil {
		t.Fatal(err)
	}
	wifi, err := resolve(t, f, androidOnline("192.168.1.9:5555"))
	if err != nil {
		t.Fatal(err)
	}
	if usb != wifi {
		t.Fatalf("usb %+v and wifi %+v differ", usb, wifi)
	}
}

func TestIdentity_EmulatorFallsBackToAvdNameJoinedWithAdbSerial(t *testing.T) {
	for _, generic := range []string{"", "unknown", "EMULATOR", "0123456789ABCDEF", "00000000"} {
		f := process.NewFake()
		stubProp(f, "emulator-5554", "ro.serialno", generic)
		stubProp(f, "emulator-5554", "ro.boot.qemu.avd_name", "Pixel_8")

		id, err := resolve(t, f, androidOnline("emulator-5554"))
		if err != nil {
			t.Fatalf("serial %q: %v", generic, err)
		}
		if id.HardwareID != "Pixel_8@emulator-5554" {
			t.Fatalf("serial %q: hardware id = %q", generic, id.HardwareID)
		}
	}
}

func TestIdentity_TwoEmulatorsWithSameAvdAndGenericSerialStayDistinct(t *testing.T) {
	f := process.NewFake()
	for _, s := range []string{"emulator-5554", "emulator-5556"} {
		stubProp(f, s, "ro.serialno", "")
		stubProp(f, s, "ro.boot.qemu.avd_name", "Pixel_8")
	}
	a, _ := resolve(t, f, androidOnline("emulator-5554"))
	b, _ := resolve(t, f, androidOnline("emulator-5556"))
	if a == b {
		t.Fatalf("identities collide: %+v", a)
	}
}

func TestIdentity_NoSerialAndNoAvdIsUnresolved(t *testing.T) {
	f := process.NewFake()
	stubProp(f, "x", "ro.serialno", "")
	stubProp(f, "x", "ro.boot.qemu.avd_name", "")

	_, err := resolve(t, f, androidOnline("x"))
	var unresolved *IdentityUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v, want IdentityUnresolvedError", err)
	}
}

func TestIdentity_UnauthorizedAndOfflineReturnTypedStateError(t *testing.T) {
	for _, state := range []State{StateUnauthorized, StateOffline, StateUnavailable} {
		f := process.NewFake()
		d := Descriptor{ID: "R58N90", Platform: PlatformAndroid, State: state}

		_, err := resolve(t, f, d)
		var se *DeviceStateError
		if !errors.As(err, &se) || se.State != state {
			t.Fatalf("state %s: err = %v", state, err)
		}
		if len(f.Invocations()) != 0 {
			t.Fatalf("state %s: adb was invoked", state)
		}
	}
}

func TestIdentity_AdbReportingUnauthorizedMapsToStateError(t *testing.T) {
	f := process.NewFake()
	f.Stub(process.ExactMatch("adb", "-s", "R58N90", "shell", "getprop", "ro.serialno"),
		process.Result{Stderr: "error: device unauthorized.", ExitCode: 1}, errors.New("exit status 1"))

	_, err := resolve(t, f, androidOnline("R58N90"))
	var se *DeviceStateError
	if !errors.As(err, &se) || se.State != StateUnauthorized {
		t.Fatalf("err = %v", err)
	}
}

func TestIdentity_OtherAdbFailureIsNotMistakenForState(t *testing.T) {
	f := process.NewFake()
	f.Stub(process.ExactMatch("adb", "-s", "R58N90", "shell", "getprop", "ro.serialno"),
		process.Result{Stderr: "adb: no such thing"}, errors.New("exit status 1"))

	_, err := resolve(t, f, androidOnline("R58N90"))
	var se *DeviceStateError
	if err == nil || errors.As(err, &se) {
		t.Fatalf("err = %v", err)
	}
}

func TestIdentity_IOSUsesDescriptorUDID(t *testing.T) {
	id, err := resolve(t, process.NewFake(), Descriptor{ID: "UDID-1", Platform: PlatformIOS})
	if err != nil {
		t.Fatal(err)
	}
	if id != (Identity{Platform: PlatformIOS, HardwareID: "UDID-1"}) {
		t.Fatalf("identity = %+v", id)
	}
}

func TestIdentity_IOSWithoutUDIDIsUnresolved(t *testing.T) {
	_, err := resolve(t, process.NewFake(), Descriptor{Platform: PlatformIOS})
	var unresolved *IdentityUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v", err)
	}
}
