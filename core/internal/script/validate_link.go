package script

import (
	"strings"

	"dev.atomyx/internal/driver"
)

// validateOpenLinkStep rejects a link the device could never open. A
// link that still holds a `${name}` placeholder is not checked, because
// the variable that completes it is known at run time, and the full
// check runs again before the device is touched.
func validateOpenLinkStep(s *OpenLinkStep, path string, report reportFunc) {
	if !strings.Contains(s.Link, "${") {
		if err := driver.ValidateLink(s.Link); err != nil {
			report("%s.link: %s", path, err.Error())
		}
	}
	validateAppID(s.AppID, path, report)
}
