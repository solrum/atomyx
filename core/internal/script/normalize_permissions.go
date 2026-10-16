package script

import (
	"sort"
	"strings"

	"dev.atomyx/internal/driver"
)

// normalizeSetPermissions accepts an object with a required
// `permissions` mapping and an optional `appId` naming the app the
// permissions belong to.
func normalizeSetPermissions(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("setPermissions: expected an object with `permissions` and an optional `appId`, got %s", goType(value))
	}
	raw, present := m["permissions"]
	if !present || raw == nil {
		return nil, newParseError("setPermissions: the object needs a `permissions` mapping")
	}
	permissions, err := normalizePermissions(raw, "setPermissions.permissions")
	if err != nil {
		return nil, err
	}
	if len(permissions) == 0 {
		return nil, newParseError("setPermissions.permissions: needs at least one permission")
	}
	step := &SetPermissionsStep{Command: CommandSetPermissions, Permissions: permissions}
	if rawAppID, present := m["appId"]; present && rawAppID != nil {
		appID, ok := asString(rawAppID)
		if !ok {
			return nil, newParseError("setPermissions.appId: must be a string, got %s", goType(rawAppID))
		}
		step.AppID = &appID
	}
	return step, nil
}

// normalizePermissions reads a mapping from permission name to state. A
// name outside the known permissions or a state other than allow or deny
// is a parse error that lists the accepted words. It returns nil when
// raw is nil or an empty mapping.
func normalizePermissions(raw any, path string) (map[driver.Permission]driver.PermissionState, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("%s: must be a mapping of permission names to allow or deny, got %s", path, goType(raw))
	}
	if len(m) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	permissions := make(map[driver.Permission]driver.PermissionState, len(m))
	for _, name := range names {
		permission, ok := driver.ParsePermission(name)
		if !ok {
			return nil, newParseError("%s.%s: unknown permission, use one of %s", path, name, acceptedPermissionNames())
		}
		stateText, ok := asString(m[name])
		if !ok {
			return nil, newParseError("%s.%s: must be allow or deny, got %s", path, name, goType(m[name]))
		}
		state, ok := driver.ParsePermissionState(stateText)
		if !ok {
			return nil, newParseError("%s.%s: %q is not a permission state, use allow or deny", path, name, stateText)
		}
		permissions[permission] = state
	}
	return permissions, nil
}

func acceptedPermissionNames() string {
	names := make([]string, 0, len(driver.AllPermissions()))
	for _, p := range driver.AllPermissions() {
		names = append(names, string(p))
	}
	return strings.Join(names, ", ")
}
