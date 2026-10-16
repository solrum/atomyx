package orchestra

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// SetPermissions grants or revokes permissions of appID without a
// prompt. An empty appID, an empty map, an unknown permission name or an
// unknown state fails with an error coded diag.CodeInvalidAppID or
// diag.CodeInvalidArgument before any device call. It fails with
// diag.CodePermissionsUnsupported when the device cannot set permissions.
//
// An app that is running may keep its earlier view of a permission, so a
// caller sets permissions before launching the app.
func (o *Orchestra) SetPermissions(ctx context.Context, appID string, permissions map[driver.Permission]driver.PermissionState) error {
	if err := validateAppID(appID); err != nil {
		return err
	}
	if err := driver.ValidatePermissions(permissions); err != nil {
		return err
	}
	if !o.driver.Capabilities().CanSetPermissions {
		return diag.NewPermissionsUnsupported("")
	}
	return o.driver.SetPermissions(ctx, appID, permissions)
}
