package script

import "dev.atomyx/internal/driver"

// validateSetOrientationStep rejects an orientation no device offers.
// The parser already rejects unknown names, so this check guards a step
// built without the parser.
func validateSetOrientationStep(s *SetOrientationStep, path string, report reportFunc) {
	if err := driver.ValidateOrientation(s.Orientation); err != nil {
		report("%s.orientation: %s", path, err.Error())
	}
}
