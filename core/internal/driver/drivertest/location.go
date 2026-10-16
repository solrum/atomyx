package drivertest

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// The positions the location cases set. They are two distinct points far
// apart, so a device that ignored the second call would be a visible
// regression for anyone who reads the position back by hand.
var (
	firstLocation  = driver.Location{Latitude: 37.3349, Longitude: -122.0090}
	secondLocation = driver.Location{Latitude: -33.8568, Longitude: 151.2153}
)

// testSetLocationMatchesCapability checks the unsupported-versus-failed
// split. A driver that reports CanSetLocation false must fail
// SetLocation with diag.CodeLocationUnsupported, and one that reports it
// true must reject a latitude outside its range with
// diag.CodeInvalidArgument before it touches the device.
func testSetLocationMatchesCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	err := d.SetLocation(context.Background(), driver.Location{Latitude: 91, Longitude: 0})
	want := diag.CodeInvalidArgument
	if !d.Capabilities().CanSetLocation {
		want = diag.CodeLocationUnsupported
	}
	if err == nil {
		t.Fatalf("SetLocation with a latitude of 91 returned no error, want code %q", want)
	}
	if code, _, _ := diag.Describe(err); code != want {
		t.Fatalf("SetLocation with a latitude of 91 failed with code %q (err %v), want %q", code, err, want)
	}
}

// testSetLocationReachesTheDevice sets two positions in turn and expects
// both calls to succeed. A driver that reports CanSetLocation false must
// instead fail the call with diag.CodeLocationUnsupported, which this
// case asserts rather than skips.
//
// The case does not read the position back. The port has no getter, and
// the demo app shows only the state of its location permission, so the
// shared suite proves that the device accepted the position and leaves
// the app-side effect to the adapter tests.
func testSetLocationReachesTheDevice(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.Capabilities().CanSetLocation {
		err := d.SetLocation(ctx, firstLocation)
		if code, _, _ := diag.Describe(err); code != diag.CodeLocationUnsupported {
			t.Fatalf("SetLocation on a driver without the capability failed with code %q (err %v), want %q", code, err, diag.CodeLocationUnsupported)
		}
		return
	}
	for _, location := range []driver.Location{firstLocation, secondLocation} {
		if err := d.SetLocation(ctx, location); err != nil {
			t.Fatalf("SetLocation(%+v): %v", location, err)
		}
	}
}
