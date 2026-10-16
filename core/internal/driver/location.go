package driver

import (
	"fmt"
	"math"

	"dev.atomyx/internal/diag"
)

// Location is a geographic position in decimal degrees on the WGS 84
// datum. Latitude is positive north of the equator and longitude is
// positive east of the prime meridian.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

const (
	// MaxLatitude is the largest latitude magnitude in degrees.
	MaxLatitude = 90

	// MaxLongitude is the largest longitude magnitude in degrees.
	MaxLongitude = 180

	// earthRadiusMeters is the mean Earth radius the great-circle
	// distance uses.
	earthRadiusMeters = 6371008.8
)

// ValidateLocation rejects a position that is not finite or lies outside
// the latitude range of -90 to 90 degrees or the longitude range of -180
// to 180 degrees. The error carries diag.CodeInvalidArgument and states
// the accepted range.
func ValidateLocation(l Location) error {
	if !isFinite(l.Latitude) || math.Abs(l.Latitude) > MaxLatitude {
		return invalidLocation(fmt.Sprintf("latitude %v is not a number from -%d to %d", l.Latitude, MaxLatitude, MaxLatitude))
	}
	if !isFinite(l.Longitude) || math.Abs(l.Longitude) > MaxLongitude {
		return invalidLocation(fmt.Sprintf("longitude %v is not a number from -%d to %d", l.Longitude, MaxLongitude, MaxLongitude))
	}
	return nil
}

// DistanceMeters returns the great-circle distance between a and b in
// meters, by the haversine formula on a spherical Earth. Both positions
// must pass ValidateLocation.
func DistanceMeters(a, b Location) float64 {
	lat1 := degreesToRadians(a.Latitude)
	lat2 := degreesToRadians(b.Latitude)
	deltaLat := lat2 - lat1
	deltaLon := degreesToRadians(b.Longitude - a.Longitude)
	h := math.Pow(math.Sin(deltaLat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(deltaLon/2), 2)
	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(h)))
}

func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func degreesToRadians(degrees float64) float64 { return degrees * math.Pi / 180 }

func invalidLocation(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
