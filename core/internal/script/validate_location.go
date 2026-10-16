package script

import "dev.atomyx/internal/driver"

// validateSetLocationStep rejects a position outside the latitude and
// longitude ranges. The parser accepts any finite number, so this check
// is what catches a coordinate that no place has.
func validateSetLocationStep(s *SetLocationStep, path string, report reportFunc) {
	if err := driver.ValidateLocation(driver.Location{Latitude: s.Latitude, Longitude: s.Longitude}); err != nil {
		report("%s: %s", path, err.Error())
	}
}

// validateTravelStep rejects a route the device could not follow: too
// few or too many points, a point outside the coordinate ranges, a speed
// that is not above zero, or a route that takes longer than the travel
// time limit.
func validateTravelStep(s *TravelStep, path string, report reportFunc) {
	if err := driver.ValidateTravel(s.Points, s.Speed); err != nil {
		report("%s: %s", path, err.Error())
	}
}
