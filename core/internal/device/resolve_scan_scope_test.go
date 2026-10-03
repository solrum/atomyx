package device

import (
	"context"
	"sync/atomic"
	"testing"
)

// countingProbe reports a fixed list and counts its scans. Its platform
// is empty for an unscoped probe.
type countingProbe struct {
	platform Platform
	result   []Descriptor
	scans    atomic.Int32
}

func (p *countingProbe) Scan(context.Context) []Descriptor {
	p.scans.Add(1)
	return p.result
}

type scopedCountingProbe struct{ *countingProbe }

func (p scopedCountingProbe) ProbePlatform() Platform { return p.platform }

func TestResolveDescriptor_AndroidScansOnlyTheAndroidProbe(t *testing.T) {
	android := &countingProbe{platform: PlatformAndroid, result: []Descriptor{
		{ID: "R58M", Platform: PlatformAndroid, Kind: KindDevice, State: StateOnline},
	}}
	ios := &countingProbe{platform: PlatformIOS, result: []Descriptor{
		{ID: "SIM-UDID", Platform: PlatformIOS, Kind: KindSimulator, State: StateOnline},
	}}
	composite := NewCompositeProbe(nil, scopedCountingProbe{android}, scopedCountingProbe{ios})

	d, err := ResolveDescriptor(context.Background(), composite, nil, PlatformAndroid, "R58M")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindDevice {
		t.Fatalf("kind = %q, want the kind the Android probe reports", d.Kind)
	}
	if got := android.scans.Load(); got != 1 {
		t.Fatalf("Android probe scans = %d, want 1", got)
	}
	if got := ios.scans.Load(); got != 0 {
		t.Fatalf("iOS probe scans = %d, want 0 when resolving an Android device", got)
	}
}

func TestResolveDescriptor_IosStillScansTheIosProbe(t *testing.T) {
	android := &countingProbe{platform: PlatformAndroid}
	ios := &countingProbe{platform: PlatformIOS, result: []Descriptor{
		{ID: "SIM-UDID", Platform: PlatformIOS, Kind: KindSimulator, State: StateOnline},
	}}
	composite := NewCompositeProbe(nil, scopedCountingProbe{android}, scopedCountingProbe{ios})

	d, err := ResolveDescriptor(context.Background(), composite, nil, PlatformIOS, "SIM-UDID")
	if err != nil || d.Kind != KindSimulator {
		t.Fatalf("got %+v, %v, want the simulator", d, err)
	}
	if got := ios.scans.Load(); got != 1 {
		t.Fatalf("iOS probe scans = %d, want 1", got)
	}
}

func TestCompositeProbe_ScanPlatform_StillScansUnscopedProbes(t *testing.T) {
	unscoped := &countingProbe{result: []Descriptor{{ID: "any", Platform: PlatformAndroid}}}
	ios := &countingProbe{platform: PlatformIOS}
	composite := NewCompositeProbe(nil, unscoped, scopedCountingProbe{ios})

	got := composite.ScanPlatform(context.Background(), PlatformAndroid)
	if len(got) != 1 || got[0].ID != "any" {
		t.Fatalf("ScanPlatform() = %+v, want the unscoped probe's device", got)
	}
	if ios.scans.Load() != 0 {
		t.Fatal("a probe scoped to iOS must not run for an Android scan")
	}
}
