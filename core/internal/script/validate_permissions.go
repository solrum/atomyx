package script

import "dev.atomyx/internal/driver"

// validateSetPermissionsStep rejects a step the device could never
// apply. The parser already rejects unknown names and states, so this
// check guards a step built without the parser.
func validateSetPermissionsStep(s *SetPermissionsStep, path string, report reportFunc) {
	if err := driver.ValidatePermissions(s.Permissions); err != nil {
		report("%s.permissions: %s", path, err.Error())
	}
	validateAppID(s.AppID, path, report)
}
