package mcp

import "dev.atomyx/internal/driver"

// permissionsSchema describes the `permissions` object of launch_app:
// one optional property per permission name, each `allow` or `deny`.
// Unknown names are rejected, so a misspelled permission fails loudly
// instead of being ignored.
func permissionsSchema() map[string]any {
	state := jsAnyOf(jsConst(string(driver.PermissionAllow)), jsConst(string(driver.PermissionDeny)))
	properties := make(map[string]any, len(driver.AllPermissions()))
	for _, p := range driver.AllPermissions() {
		properties[string(p)] = state
	}
	schema := jsObject(properties)
	schema["additionalProperties"] = false
	schema["description"] = "Permissions to grant (`allow`) or revoke (`deny`) before the app starts, " +
		"keyed by permission name: camera, location, microphone, photos, contacts, calendar, " +
		"notifications. The launch always reaches the device, even when the app was launched " +
		"before. A device that cannot set a permission fails with `PERMISSIONS_UNSUPPORTED`, and " +
		"a request that changed nothing fails with `PERMISSION_NOT_SET`. A launch with `attach` " +
		"that finds the app running may leave the app with its earlier view of a permission."
	return schema
}

// launchPermissions converts the validated permissions argument to the
// driver's permission map. It returns nil when the argument is absent or
// empty.
func launchPermissions(raw map[string]string) map[driver.Permission]driver.PermissionState {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[driver.Permission]driver.PermissionState, len(raw))
	for name, state := range raw {
		out[driver.Permission(name)] = driver.PermissionState(state)
	}
	return out
}
