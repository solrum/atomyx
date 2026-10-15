package drivertest

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

func testScreenSizePositiveAndStable(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	first, err := d.ScreenSize(ctx)
	if err != nil {
		t.Fatalf("ScreenSize: %v", err)
	}
	if first.Width <= 0 || first.Height <= 0 {
		t.Fatalf("ScreenSize returned non-positive dimensions: %+v", first)
	}
	second, err := d.ScreenSize(ctx)
	if err != nil {
		t.Fatalf("second ScreenSize: %v", err)
	}
	if second != first {
		t.Fatalf("ScreenSize changed between calls with no intervening rotation or resize: %+v vs %+v", first, second)
	}
}

func testHierarchyShape(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !stageMinimalHierarchy(d, wire.TreeNodeWire{Attributes: map[string]string{}}) {
		SkipBecause(t, SkipNoHierarchyStaging, "driver offers no way to stage a hierarchy for this suite")
	}
	tree, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	if tree.Attributes == nil {
		t.Fatal("Hierarchy returned a tree with a nil Attributes map")
	}
}

// canonicalAttributeKeys is the fixed attribute vocabulary
// wire.TreeNodeWire documents: any other key must start with "ext:"
// to mark itself as non-portable extension data.
// testHierarchyAttributesAreCanonical calls Hierarchy without staging
// anything, so it exercises whatever tree a connected driver produces
// by default. A driver with no default tree to report (nothing
// staged yet) skips rather than fails, since that is a valid,
// documented state for a scripted subject. Every node's Attributes
// map, recursively, must be non-nil and use only the canonical key
// set or an "ext:"-prefixed key: no platform-native field name may
// leak past a driver's normalization.
func testHierarchyAttributesAreCanonical(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	tree, err := d.Hierarchy(ctx)
	if err != nil {
		SkipBecause(t, SkipNoDefaultHierarchy, "driver has no default hierarchy to inspect without staging one")
	}
	assertCanonicalAttributes(t, tree)
}

func assertCanonicalAttributes(t *testing.T, node wire.TreeNodeWire) {
	t.Helper()
	if node.Attributes == nil {
		t.Fatal("Hierarchy returned a node with a nil Attributes map")
	}
	for key := range node.Attributes {
		if wire.IsCanonicalAttribute(key) {
			continue
		}
		if strings.HasPrefix(key, "ext:") {
			continue
		}
		t.Fatalf("Hierarchy returned a non-canonical, non-ext-prefixed attribute key %q", key)
	}
	for _, child := range node.Children {
		assertCanonicalAttributes(t, child)
	}
}

// testDeviceInfoContract asserts the fixed relationships
// Driver.DeviceInfo's fields carry regardless of platform: Platform
// matches Platform(), UDID identifies a device, and Kind is one of
// the named DeviceKind values.
func testDeviceInfoContract(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	info, err := d.DeviceInfo(ctx)
	if err != nil {
		t.Fatalf("DeviceInfo: %v", err)
	}
	if info.Platform != d.Platform() {
		t.Fatalf("DeviceInfo().Platform = %q, want %q (Platform())", info.Platform, d.Platform())
	}
	if info.UDID == "" {
		t.Fatal("DeviceInfo().UDID is empty")
	}
	switch info.Kind {
	case driver.DeviceKindSimulator, driver.DeviceKindEmulator, driver.DeviceKindDevice:
	default:
		t.Fatalf("DeviceInfo().Kind = %q, want one of simulator, emulator, device", info.Kind)
	}
}

// testScreenshotCapability asserts the error-or-succeed half of
// Driver.Screenshot's capability contract: a driver reporting
// CanScreenshot false must reject the call, and a driver reporting it
// true must not. It does not assert on the payload's encoding,
// because the contract-test fixtures across every adapter
// intentionally stand in an empty capture for a real one.
func testScreenshotCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_, err := d.Screenshot(ctx)
	if !d.Capabilities().CanScreenshot {
		if err == nil {
			t.Fatal("Screenshot succeeded on a driver reporting CanScreenshot false")
		}
		return
	}
	if err != nil {
		t.Fatalf("Screenshot on a driver reporting CanScreenshot true: %v", err)
	}
}
