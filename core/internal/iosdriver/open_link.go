package iosdriver

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// unsupportedOSReason is the reason the runner reports when the device
// OS predates the XCUITest link API.
const unsupportedOSReason = "unsupported-os"

type openLinkResponse struct {
	Opened bool   `json:"opened"`
	Reason string `json:"reason"`
}

// OpenLink asks the runner to open link through the system, or in
// opts.AppID when it is set. A device OS older than iOS 16.4 fails with
// diag.CodeLinkUnsupported. When the runner opens the link in a named
// app, the driver treats that app as the one last launched, because the
// runner tracks it as the app under test.
func (d *IosDriver) OpenLink(ctx context.Context, link string, opts driver.OpenLinkOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := driver.ValidateLink(link); err != nil {
		return err
	}
	params := map[string]any{"url": link}
	if opts.AppID != "" {
		params["bundleId"] = opts.AppID
	}
	var resp openLinkResponse
	if err := d.conn.CallInto(ctx, "openLink", params, &resp); err != nil {
		return d.fail(err)
	}
	if !resp.Opened {
		if resp.Reason == unsupportedOSReason {
			return diag.NewLinkUnsupported("this device needs iOS 16.4 or later")
		}
		return diag.NewLinkNotOpened("")
	}
	if opts.AppID != "" {
		d.lastLaunchedBundleID = opts.AppID
	}
	return nil
}
