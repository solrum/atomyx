package drivertest

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// OpenLinkFixture describes a link that visibly lands the launchable app
// on a known screen, so a case can prove the link reached the app and not
// only the driver.
type OpenLinkFixture struct {
	// Link is handed to Driver.OpenLink.
	Link string

	// AppID names the app that must open Link. Naming it keeps a custom
	// scheme from raising a system confirmation prompt.
	AppID string

	// Await must block until the app shows the screen Link opens and
	// fail the test when it does not. It observes the screen through d.
	Await func(t *testing.T, d driver.Driver)
}

// testOpenLinkMatchesCapability checks the unsupported-versus-failed
// split. A driver that reports CanOpenLink false must fail OpenLink with
// diag.CodeLinkUnsupported, and one that reports it true must reject a
// link without a scheme with diag.CodeInvalidArgument before it touches
// the device.
func testOpenLinkMatchesCapability(t *testing.T, newDriver Factory) {
	d := newDriver()
	ctx := context.Background()
	err := d.OpenLink(ctx, "not a link", driver.OpenLinkOptions{})
	want := diag.CodeInvalidArgument
	if !d.Capabilities().CanOpenLink {
		want = diag.CodeLinkUnsupported
	}
	if err == nil {
		t.Fatalf("OpenLink with an invalid link returned no error, want code %q", want)
	}
	if code, _, _ := diag.Describe(err); code != want {
		t.Fatalf("OpenLink with an invalid link failed with code %q (err %v), want %q", code, err, want)
	}
}

// testOpenLinkReachesTheApp starts the launchable app on its home
// screen, opens the fixture link, and waits for the app to show the
// link's destination. It skips when the driver is wired without a link
// fixture or cannot open links.
func testOpenLinkReachesTheApp(t *testing.T, newDriver Factory, hooks Hooks) {
	bundleID := requireLaunchable(t, hooks)
	if hooks.OpenLink == nil {
		SkipBecause(t, SkipNoOpenLinkFixture, "no OpenLink fixture wired for this driver")
	}
	d := newDriver()
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !d.Capabilities().CanOpenLink {
		SkipBecause(t, SkipNoOpenLink, "driver reports CanOpenLink false")
	}
	fresh := false
	if err := d.LaunchApp(ctx, bundleID, driver.LaunchArgs{NoReset: &fresh}); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	if err := d.OpenLink(ctx, hooks.OpenLink.Link, driver.OpenLinkOptions{AppID: hooks.OpenLink.AppID}); err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	hooks.OpenLink.Await(t, d)
}
