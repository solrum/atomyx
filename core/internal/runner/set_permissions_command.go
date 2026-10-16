package runner

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
)

// SetPermissionsCommand grants or revokes the step's permissions for the
// step's app, or the script's app when the step names none. The change is
// in place when the step succeeds, but an app that is already running may
// keep its earlier view, so a script sets permissions before it launches
// the app.
type SetPermissionsCommand struct{}

func (SetPermissionsCommand) Command() string { return script.CommandSetPermissions }

func (SetPermissionsCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.SetPermissionsStep)
	written := authored[*script.SetPermissionsStep](step)
	summary := describePermissions(s.Permissions)
	return runAppAction(s.AppID, written.AppID, "Set "+summary+" for", ctx, func(appID string) error {
		return ctx.Orchestra.SetPermissions(ctx.Context, appID, s.Permissions)
	})
}

// describePermissions renders permissions as a stable `camera=allow,
// location=deny` list.
func describePermissions(permissions map[driver.Permission]driver.PermissionState) string {
	names := driver.SortedPermissions(permissions)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%s=%s", name, permissions[name])
	}
	return strings.Join(parts, ", ")
}
