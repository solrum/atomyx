package device

import (
	"context"
	"errors"
	"testing"
)

func iosProbeFixture() Probe {
	return NewStaticProbe([]Descriptor{
		{ID: "SIM-UDID", Platform: PlatformIOS, Name: "iPhone 15", Kind: KindSimulator, State: StateOnline},
		{ID: "00008101-AAAA", Platform: PlatformIOS, Name: "Bulbasaur", Kind: KindDevice, State: StateOnline},
		{ID: "emulator-5554", Platform: PlatformAndroid, Kind: KindEmulator, State: StateOnline},
	})
}

func TestResolveDescriptor_IosSimulatorUdid(t *testing.T) {
	d, err := ResolveDescriptor(context.Background(), iosProbeFixture(), nil, PlatformIOS, "SIM-UDID")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindSimulator || d.ID != "SIM-UDID" {
		t.Fatalf("got %+v, want simulator SIM-UDID", d)
	}
}

func TestResolveDescriptor_IosPhysicalUdid(t *testing.T) {
	d, err := ResolveDescriptor(context.Background(), iosProbeFixture(), nil, PlatformIOS, "00008101-aaaa")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindDevice || d.ID != "00008101-AAAA" {
		t.Fatalf("got %+v, want physical device 00008101-AAAA", d)
	}
}

func TestResolveDescriptor_IosUnknownUdid(t *testing.T) {
	_, err := ResolveDescriptor(context.Background(), iosProbeFixture(), nil, PlatformIOS, "NOPE")
	var nf *DeviceNotFoundError
	if !errors.As(err, &nf) || nf.ID != "NOPE" {
		t.Fatalf("err = %v, want DeviceNotFoundError for NOPE", err)
	}
}

func TestResolveDescriptor_IosIgnoresAndroidIds(t *testing.T) {
	_, err := ResolveDescriptor(context.Background(), iosProbeFixture(), nil, PlatformIOS, "emulator-5554")
	if err == nil {
		t.Fatal("an Android id must not resolve as an iOS device")
	}
}

func TestResolveDescriptor_AndroidDefaultsAndPassesIdThrough(t *testing.T) {
	d, err := ResolveDescriptor(context.Background(), nil, nil, PlatformAndroid, "")
	if err != nil || d.ID != "emulator-5554" || d.Kind != KindEmulator {
		t.Fatalf("default: got %+v, %v", d, err)
	}
	d, err = ResolveDescriptor(context.Background(), nil, nil, PlatformAndroid, "192.168.1.5:5555")
	if err != nil || d.ID != "192.168.1.5:5555" {
		t.Fatalf("explicit: got %+v, %v", d, err)
	}
}

func TestResolveDescriptor_AndroidKindComesFromTheProbe(t *testing.T) {
	probe := NewStaticProbe([]Descriptor{
		{ID: "emulator-5556", Platform: PlatformAndroid, Kind: KindEmulator, State: StateOnline},
		{ID: "RF8N90WV5QD", Platform: PlatformAndroid, Kind: KindDevice, State: StateOnline},
	})
	cases := map[string]Kind{"emulator-5556": KindEmulator, "RF8N90WV5QD": KindDevice}
	for id, want := range cases {
		d, err := ResolveDescriptor(context.Background(), probe, nil, PlatformAndroid, id)
		if err != nil || d.ID != id || d.Kind != want {
			t.Errorf("%s: got %+v, %v, want kind %s", id, d, err, want)
		}
	}
}

func TestResolveDescriptor_AnUnlistedAndroidSerialKindFollowsItsName(t *testing.T) {
	probe := NewStaticProbe(nil)
	cases := map[string]Kind{
		"":                 KindEmulator,
		"emulator-5554":    KindEmulator,
		"RF8N90WV5QD":      KindDevice,
		"192.168.1.5:5555": KindDevice,
	}
	for id, want := range cases {
		d, err := ResolveDescriptor(context.Background(), probe, nil, PlatformAndroid, id)
		if err != nil || d.Kind != want {
			t.Errorf("%q: got %+v, %v, want kind %s", id, d, err, want)
		}
	}
}
