package script

import (
	"sort"
	"strconv"
	"strings"

	"dev.atomyx/internal/driver"
)

// normalizeSetLocation accepts an object with a required `latitude` and
// `longitude`, both numbers in degrees.
func normalizeSetLocation(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("setLocation: expected an object with `latitude` and `longitude`, got %s", goType(value))
	}
	if err := rejectUnknownKeys("setLocation", m, "latitude", "longitude"); err != nil {
		return nil, err
	}
	location, err := normalizeLocation("setLocation", m)
	if err != nil {
		return nil, err
	}
	return &SetLocationStep{Command: CommandSetLocation, Latitude: location.Latitude, Longitude: location.Longitude}, nil
}

// normalizeTravel accepts an object with a required `points` list, each
// point an object with `latitude` and `longitude`, and an optional
// `speed` in meters per second. A missing speed takes
// driver.DefaultTravelSpeedMetersPerSecond.
func normalizeTravel(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("travel: expected an object with `points` and an optional `speed`, got %s", goType(value))
	}
	if err := rejectUnknownKeys("travel", m, "points", "speed"); err != nil {
		return nil, err
	}
	rawPoints, present := m["points"]
	if !present || rawPoints == nil {
		return nil, newParseError("travel: the object needs a `points` list")
	}
	list, ok := asSlice(rawPoints)
	if !ok {
		return nil, newParseError("travel.points: must be a list of objects with `latitude` and `longitude`, got %s", goType(rawPoints))
	}
	points := make([]driver.Location, len(list))
	for i, raw := range list {
		path := "travel.points[" + strconv.Itoa(i) + "]"
		pm, ok := asMap(raw)
		if !ok {
			return nil, newParseError("%s: expected an object with `latitude` and `longitude`, got %s", path, goType(raw))
		}
		if err := rejectUnknownKeys(path, pm, "latitude", "longitude"); err != nil {
			return nil, err
		}
		location, err := normalizeLocation(path, pm)
		if err != nil {
			return nil, err
		}
		points[i] = location
	}
	speed := driver.DefaultTravelSpeedMetersPerSecond
	if raw, present := m["speed"]; present && raw != nil {
		speed, ok = asFiniteFloat(raw)
		if !ok {
			return nil, newParseError("travel.speed: must be a number of meters per second, got %s", goType(raw))
		}
	}
	return &TravelStep{Command: CommandTravel, Points: points, Speed: speed}, nil
}

// normalizeLocation reads the required `latitude` and `longitude`
// numbers of m. path names the object in an error message.
func normalizeLocation(path string, m map[string]any) (driver.Location, error) {
	var location driver.Location
	for _, field := range []struct {
		key string
		dst *float64
	}{{"latitude", &location.Latitude}, {"longitude", &location.Longitude}} {
		raw, present := m[field.key]
		if !present || raw == nil {
			return location, newParseError("%s: the object needs a `%s`", path, field.key)
		}
		f, ok := asFiniteFloat(raw)
		if !ok {
			return location, newParseError("%s.%s: must be a number of degrees, got %s", path, field.key, goType(raw))
		}
		*field.dst = f
	}
	return location, nil
}

// rejectUnknownKeys fails with a parse error naming the first key of m
// that is not in allowed, in sorted order.
func rejectUnknownKeys(path string, m map[string]any, allowed ...string) error {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !known[key] {
			return newParseError("%s: unknown key `%s`, use only: %s", path, key, strings.Join(allowed, ", "))
		}
	}
	return nil
}
