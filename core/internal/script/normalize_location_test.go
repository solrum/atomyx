package script

import (
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
)

func TestParseScript_SetLocationForms(t *testing.T) {
	step := mustParse(t, "- setLocation:\n    latitude: 37.7749\n    longitude: -122.4194\n").Steps[0].(*SetLocationStep)
	if step.Latitude != 37.7749 || step.Longitude != -122.4194 {
		t.Errorf("step = %+v, want 37.7749, -122.4194", step)
	}
	whole := mustParse(t, "- setLocation: {latitude: 10, longitude: -20}\n").Steps[0].(*SetLocationStep)
	if whole.Latitude != 10 || whole.Longitude != -20 {
		t.Errorf("step = %+v, want 10, -20", whole)
	}
}

func TestParseScript_TravelForms(t *testing.T) {
	step := mustParse(t, "- travel:\n    points:\n      - {latitude: 1, longitude: 2}\n      - {latitude: 1.0005, longitude: 2.0005}\n    speed: 12.5\n").Steps[0].(*TravelStep)
	want := []driver.Location{{Latitude: 1, Longitude: 2}, {Latitude: 1.0005, Longitude: 2.0005}}
	if len(step.Points) != 2 || step.Points[0] != want[0] || step.Points[1] != want[1] {
		t.Errorf("points = %v, want %v", step.Points, want)
	}
	if step.Speed != 12.5 {
		t.Errorf("speed = %v, want 12.5", step.Speed)
	}
}

func TestParseScript_TravelDefaultsTheSpeed(t *testing.T) {
	step := mustParse(t, "- travel:\n    points:\n      - {latitude: 1, longitude: 2}\n      - {latitude: 1.001, longitude: 2}\n").Steps[0].(*TravelStep)
	if step.Speed != driver.DefaultTravelSpeedMetersPerSecond {
		t.Errorf("speed = %v, want the default %v", step.Speed, driver.DefaultTravelSpeedMetersPerSecond)
	}
}

func TestParseScript_LocationRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"set bare", "- setLocation\n", "setLocation"},
		{"set string", "- setLocation: here\n", "setLocation: expected an object"},
		{"set missing longitude", "- setLocation:\n    latitude: 1\n", "needs a `longitude`"},
		{"set text latitude", "- setLocation: {latitude: north, longitude: 1}\n", "setLocation.latitude: must be a number"},
		{"set unknown key", "- setLocation: {latitude: 1, longitude: 2, altitude: 3}\n", "unknown key `altitude`"},
		{"set latitude out of range", "- setLocation: {latitude: 91, longitude: 0}\n", "latitude 91 is not a number from -90 to 90"},
		{"set longitude out of range", "- setLocation: {latitude: 0, longitude: -181}\n", "longitude -181 is not a number from -180 to 180"},
		{"set not finite", "- setLocation: {latitude: .nan, longitude: 0}\n", "setLocation.latitude: must be a number"},
		{"travel string", "- travel: home\n", "travel: expected an object"},
		{"travel no points", "- travel:\n    speed: 4\n", "needs a `points` list"},
		{"travel points not a list", "- travel:\n    points: here\n", "travel.points: must be a list"},
		{"travel point not an object", "- travel:\n    points: [1, 2]\n", "travel.points[0]: expected an object"},
		{"travel point missing key", "- travel:\n    points:\n      - {latitude: 1}\n      - {latitude: 2, longitude: 2}\n", "travel.points[0]: the object needs a `longitude`"},
		{"travel one point", "- travel:\n    points:\n      - {latitude: 1, longitude: 2}\n", "needs from 2 to 100 points"},
		{"travel speed zero", "- travel:\n    speed: 0\n    points:\n      - {latitude: 1, longitude: 2}\n      - {latitude: 1, longitude: 3}\n", "above zero"},
		{"travel speed text", "- travel:\n    speed: fast\n    points:\n      - {latitude: 1, longitude: 2}\n      - {latitude: 1, longitude: 3}\n", "travel.speed: must be a number"},
		{"travel unknown key", "- travel:\n    pace: 4\n    points: []\n", "unknown key `pace`"},
		{"travel too slow", "- travel:\n    speed: 1\n    points:\n      - {latitude: 0, longitude: 0}\n      - {latitude: 0, longitude: 10}\n", "more than the 600 second limit"},
		{"travel point out of range", "- travel:\n    points:\n      - {latitude: 1, longitude: 2}\n      - {latitude: 100, longitude: 2}\n", "point 2: latitude"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
