package iosdriver

import (
	"context"
	"fmt"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// simulatorPrivacyServices maps each neutral permission name to the
// simulator privacy service that controls it. A name with no entry has
// no service the simulator can set: it has no camera, and it offers no
// control over notification authorization.
var simulatorPrivacyServices = map[driver.Permission]string{
	driver.PermissionLocation:   "location",
	driver.PermissionMicrophone: "microphone",
	driver.PermissionPhotos:     "photos",
	driver.PermissionContacts:   "contacts",
	driver.PermissionCalendar:   "calendar",
}

// SetPermissions grants or revokes permissions of appID on a simulator
// through the host's privacy service for the simulator. The adapter
// reports CanSetPermissions only for a simulator. On a physical device
// the system has no way to change an app's permissions from outside
// the app, so the call fails with diag.CodePermissionsUnsupported.
//
// Every name is resolved before the first change, so a name the
// simulator has no service for fails with diag.CodePermissionsUnsupported
// and leaves the simulator untouched. The system may stop the app when a
// permission changes.
func (d *IosDriver) SetPermissions(ctx context.Context, appID string, permissions map[driver.Permission]driver.PermissionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.opts.Kind != KindSimulator {
		return diag.NewPermissionsUnsupported(
			"permissions cannot be set from outside the app on a physical device")
	}
	if err := driver.ValidatePermissions(permissions); err != nil {
		return err
	}
	if appID == "" || strings.HasPrefix(appID, "-") {
		return diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("%q is not a valid app id", appID),
			diag.InvalidArgumentRemedy, nil)
	}
	names := driver.SortedPermissions(permissions)
	services := make([]string, len(names))
	for i, name := range names {
		service, ok := simulatorPrivacyServices[name]
		if !ok {
			return diag.NewPermissionsUnsupported(fmt.Sprintf(
				"the %s permission cannot be set on a simulator", name))
		}
		services[i] = service
	}
	for i, name := range names {
		action := "grant"
		if permissions[name] == driver.PermissionDeny {
			action = "revoke"
		}
		res, err := d.proc.Run(ctx, "xcrun", "simctl", "privacy", d.opts.UDID, action, services[i], appID)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
				return err
			}
			return diag.NewPermissionNotSet(fmt.Sprintf("the simulator refused to %s %s: %s",
				action, name, firstNonEmptyLine(res.Stderr, res.Stdout)), err)
		}
	}
	return nil
}

// firstNonEmptyLine returns the first non-empty line of the first
// output that has one, or a placeholder when every output is empty.
func firstNonEmptyLine(outputs ...string) string {
	for _, output := range outputs {
		for _, line := range strings.Split(output, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return "no reason given"
}
