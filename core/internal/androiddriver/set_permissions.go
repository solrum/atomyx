package androiddriver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

const sdkLevelProperty = "ro.build.version.sdk"

// permissionNotRequestedMarker is the text the package manager reports
// when asked to change a permission the app does not declare.
const permissionNotRequestedMarker = "not requested permission"

// SetPermissions grants or revokes the runtime permissions each neutral
// name maps to for appID, through the package manager on the host shell.
//
// Every name is resolved against the device's API level before the first
// change, so a name the device has no permission for fails with
// diag.CodePermissionsUnsupported and leaves the device untouched. A
// permission of a group that the app does not declare is skipped, and
// the call fails with diag.CodePermissionNotSet only when the app
// declares none of the group's permissions.
func (d *AndroidDriver) SetPermissions(ctx context.Context, appID string, permissions map[driver.Permission]driver.PermissionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := driver.ValidatePermissions(permissions); err != nil {
		return err
	}
	if !androidPackageNamePattern.MatchString(appID) {
		return diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("%q is not a valid app id", appID),
			diag.InvalidArgumentRemedy, nil)
	}
	sdk, err := d.sdkLevel(ctx)
	if err != nil {
		return err
	}
	groups := make([][]string, 0, len(permissions))
	for _, name := range driver.SortedPermissions(permissions) {
		members, ok := androidPermissionsFor(name, sdk)
		if !ok {
			return diag.NewPermissionsUnsupported(fmt.Sprintf(
				"the %s permission cannot be set because this device's software has no such permission", name))
		}
		groups = append(groups, members)
	}
	for i, name := range driver.SortedPermissions(permissions) {
		if err := d.applyPermissionGroup(ctx, appID, name, permissions[name], groups[i]); err != nil {
			return err
		}
	}
	return nil
}

// applyPermissionGroup changes every member of one group and counts the
// members the app declared.
func (d *AndroidDriver) applyPermissionGroup(ctx context.Context, appID string, name driver.Permission, state driver.PermissionState, members []string) error {
	verb := "grant"
	if state == driver.PermissionDeny {
		verb = "revoke"
	}
	changed := 0
	for _, member := range members {
		res, err := d.proc.Run(ctx, "adb", permissionArguments(d.opts.Serial, verb, appID, member)...)
		if err == nil {
			changed++
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
			return err
		}
		reason := firstOutputLine(res.Stderr, res.Stdout)
		if adbDeviceUnreachable(reason) {
			return d.unreachable(err)
		}
		if strings.Contains(strings.ToLower(res.Stderr+"\n"+res.Stdout), permissionNotRequestedMarker) {
			continue
		}
		return diag.NewPermissionNotSet(fmt.Sprintf("the device refused to %s %s: %s", verb, name, permissionFailureReason(reason)), err)
	}
	if changed == 0 {
		return diag.NewPermissionNotSet(fmt.Sprintf("the app does not declare the %s permission", name), nil)
	}
	return nil
}

// sdkLevel reads the device's API level.
func (d *AndroidDriver) sdkLevel(ctx context.Context) (int, error) {
	res, err := d.proc.Run(ctx, "adb", "-s", d.opts.Serial, "shell", "getprop", sdkLevelProperty)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
			return 0, err
		}
		if adbDeviceUnreachable(firstOutputLine(res.Stderr, res.Stdout)) {
			return 0, d.unreachable(err)
		}
		return 0, diag.NewPermissionNotSet("could not read the device software level", err)
	}
	sdk, convErr := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if convErr != nil {
		return 0, diag.NewPermissionNotSet("could not read the device software level", convErr)
	}
	return sdk, nil
}

// permissionArguments builds the adb command line that changes one
// permission. adb joins every word after "shell" into one string for the
// device shell, so the package and permission names are single-quoted to
// reach the package manager as one word each.
func permissionArguments(serial, verb, appID, permission string) []string {
	return []string{"-s", serial, "shell", "pm", verb, shellQuote(appID), shellQuote(permission)}
}

// permissionFailureReason keeps the device's own text out of the message
// when it is empty, so the sentence still reads whole.
func permissionFailureReason(deviceLine string) string {
	if deviceLine == "" {
		return "no reason given"
	}
	return deviceLine
}
