package driver

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestTravelLegDelaysMs(t *testing.T) {
	points := []Location{{0, 0}, {0, 1}, {0, 1}}
	got := TravelLegDelaysMs(points, 111195)
	if len(got) != 2 {
		t.Fatalf("delays = %v, want 2 entries", got)
	}
	if got[0] < 999 || got[0] > 1002 {
		t.Errorf("first leg = %dms, want about 1000ms", got[0])
	}
	if got[1] != 0 {
		t.Errorf("second leg = %dms, want 0 for a repeated point", got[1])
	}
	if TravelLegDelaysMs(points[:1], 4) != nil {
		t.Error("one point has no legs")
	}
}

func TestValidateTravel(t *testing.T) {
	near := []Location{{0, 0}, {0, 0.001}}
	cases := []struct {
		name   string
		points []Location
		speed  float64
		want   string
	}{
		{"valid", near, 4, ""},
		{"one point", near[:1], 4, "needs from 2 to 100 points"},
		{"no points", nil, 4, "needs from 2 to 100 points"},
		{"too many", make([]Location, MaxTravelPoints+1), 4, "needs from 2 to 100 points"},
		{"zero speed", near, 0, "meters per second above zero"},
		{"negative speed", near, -1, "meters per second above zero"},
		{"bad point", []Location{{0, 0}, {95, 0}}, 4, "point 2: latitude"},
		{"too slow", []Location{{0, 0}, {0, 10}}, 4, "more than the 600 second limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTravel(tc.points, tc.speed)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateTravel = %v, want nil", err)
				}
				return
			}
			code, message, _ := diag.Describe(err)
			if code != diag.CodeInvalidArgument || !strings.Contains(message, tc.want) {
				t.Fatalf("ValidateTravel = %q %q, want %q containing %q", code, message, diag.CodeInvalidArgument, tc.want)
			}
		})
	}
}
