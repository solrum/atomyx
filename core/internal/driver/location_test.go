package driver

import (
	"math"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestValidateLocation(t *testing.T) {
	valid := []Location{
		{0, 0},
		{90, 180},
		{-90, -180},
		{37.7749, -122.4194},
	}
	for _, l := range valid {
		if err := ValidateLocation(l); err != nil {
			t.Errorf("ValidateLocation(%+v) = %v, want nil", l, err)
		}
	}
	invalid := []Location{
		{90.0001, 0},
		{-91, 0},
		{0, 180.5},
		{0, -181},
		{math.NaN(), 0},
		{0, math.Inf(1)},
	}
	for _, l := range invalid {
		err := ValidateLocation(l)
		if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
			t.Errorf("ValidateLocation(%+v) code = %q (err %v), want %q", l, code, err, diag.CodeInvalidArgument)
		}
	}
}

func TestDistanceMeters(t *testing.T) {
	cases := []struct {
		name string
		a, b Location
		want float64
		tol  float64
	}{
		{"same point", Location{10, 20}, Location{10, 20}, 0, 1e-6},
		{"one degree of latitude", Location{0, 0}, Location{1, 0}, 111195, 10},
		{"one degree of longitude on the equator", Location{0, 0}, Location{0, 1}, 111195, 10},
		{"paris to london", Location{48.8566, 2.3522}, Location{51.5074, -0.1278}, 343500, 1000},
		{"antipodes", Location{0, 0}, Location{0, 180}, math.Pi * earthRadiusMeters, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DistanceMeters(tc.a, tc.b)
			if math.Abs(got-tc.want) > tc.tol {
				t.Errorf("DistanceMeters = %.1f, want %.1f within %.1f", got, tc.want, tc.tol)
			}
			if back := DistanceMeters(tc.b, tc.a); math.Abs(back-got) > 1e-6 {
				t.Errorf("distance is not symmetric: %.6f and %.6f", got, back)
			}
		})
	}
}
