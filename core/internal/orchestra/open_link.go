package orchestra

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// OpenLink opens link, a URL, deep link or universal link, in the
// device's handler for it, or in appID when that is not empty.
//
// It fails with an error coded diag.CodeInvalidArgument for an invalid
// link and diag.CodeInvalidAppID for a malformed appID, both before any
// device call. It fails with diag.CodeLinkUnsupported when the device
// cannot open links. Success means the device accepted the link, not
// that a screen finished appearing, so a caller that needs the
// destination waits for an element on it.
//
// Opening a link in a named app marks that app launched in the current
// flow, so a later default launch of it is not repeated.
func (o *Orchestra) OpenLink(ctx context.Context, link, appID string) error {
	if err := driver.ValidateLink(link); err != nil {
		return err
	}
	if appID != "" {
		if err := validateAppID(appID); err != nil {
			return err
		}
	}
	if !o.driver.Capabilities().CanOpenLink {
		return diag.NewLinkUnsupported("")
	}
	if err := o.driver.OpenLink(ctx, link, driver.OpenLinkOptions{AppID: appID}); err != nil {
		return err
	}
	if appID != "" {
		o.recordLaunch(appID)
	}
	return nil
}

// launchAppWithLink starts appID by opening opts.Link in it. The link
// replaces the launch, so a mode other than the default and launch
// arguments, which both shape a launch that does not happen, are
// rejected before any device call.
func (o *Orchestra) launchAppWithLink(ctx context.Context, appID string, opts LaunchOptions) error {
	if err := validateAppID(appID); err != nil {
		return err
	}
	if (opts.Mode != "" && opts.Mode != LaunchModeDefault) || len(opts.Arguments) > 0 {
		return diag.New(
			diag.CodeInvalidArgument,
			"a launch with a link cannot also set a launch mode or arguments",
			"drop the link to launch with a mode or arguments, or drop them to open the link",
			nil,
		)
	}
	if len(opts.Permissions) > 0 {
		if err := driver.ValidateLink(opts.Link); err != nil {
			return err
		}
		if err := o.SetPermissions(ctx, appID, opts.Permissions); err != nil {
			return err
		}
	}
	return o.OpenLink(ctx, opts.Link, appID)
}
