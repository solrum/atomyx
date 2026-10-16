package driver

import (
	"fmt"
	"math"

	"dev.atomyx/internal/diag"
)

const (
	// DefaultTravelSpeedMetersPerSecond is the speed a travel uses when
	// the caller names none, about a brisk walk.
	DefaultTravelSpeedMetersPerSecond = 4.0

	// MinTravelPoints is the fewest points a travel needs, a start and
	// one place to go.
	MinTravelPoints = 2

	// MaxTravelPoints bounds how many positions one travel sets.
	MaxTravelPoints = 100

	// MaxTravelDurationMs bounds the time one travel spends moving
	// between its points, so a typo in a speed or a coordinate cannot
	// hold a run for hours.
	MaxTravelDurationMs = 10 * 60 * 1000
)

// ValidateTravel rejects a travel that cannot run. It needs between
// MinTravelPoints and MaxTravelPoints valid positions, a finite speed
// above zero, and a total time at that speed within MaxTravelDurationMs.
// The error carries diag.CodeInvalidArgument.
func ValidateTravel(points []Location, speedMetersPerSecond float64) error {
	if len(points) < MinTravelPoints || len(points) > MaxTravelPoints {
		return invalidTravel(fmt.Sprintf("a travel needs from %d to %d points, got %d", MinTravelPoints, MaxTravelPoints, len(points)))
	}
	if !isFinite(speedMetersPerSecond) || speedMetersPerSecond <= 0 {
		return invalidTravel(fmt.Sprintf("speed %v is not a number of meters per second above zero", speedMetersPerSecond))
	}
	for i, p := range points {
		if err := ValidateLocation(p); err != nil {
			_, message, _ := diag.Describe(err)
			return invalidTravel(fmt.Sprintf("point %d: %s", i+1, message))
		}
	}
	var total int64
	for _, delay := range TravelLegDelaysMs(points, speedMetersPerSecond) {
		total += delay
	}
	if total > MaxTravelDurationMs {
		return invalidTravel(fmt.Sprintf("the route takes %d seconds at %v meters per second, more than the %d second limit",
			total/1000, speedMetersPerSecond, MaxTravelDurationMs/1000))
	}
	return nil
}

// TravelLegDelaysMs returns, for each point after the first, the time in
// milliseconds a traveler at speedMetersPerSecond needs to cover the
// great-circle distance from the point before it, rounded up. The result
// has one entry fewer than points. The points and the speed must pass
// ValidateTravel's per-value checks.
func TravelLegDelaysMs(points []Location, speedMetersPerSecond float64) []int64 {
	if len(points) < 2 {
		return nil
	}
	delays := make([]int64, len(points)-1)
	for i := 1; i < len(points); i++ {
		seconds := DistanceMeters(points[i-1], points[i]) / speedMetersPerSecond
		delays[i-1] = int64(math.Ceil(seconds * 1000))
	}
	return delays
}

func invalidTravel(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
