package driver

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

func TestValidateGesture(t *testing.T) {
	pt := wire.Point{X: 1, Y: 1}
	down := GestureWaypoint{Phase: GesturePhaseDown, Point: pt}
	up := GestureWaypoint{Phase: GesturePhaseUp, Point: pt, AtOffsetSeconds: 0.1}
	bad := 1.5
	cases := []struct {
		name    string
		g       Gesture
		wantErr bool
	}{
		{"valid tap", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{down, up}}}}, false},
		{"no pointers", Gesture{}, true},
		{"single waypoint", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{down}}}}, true},
		{"starts with move", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{{Phase: GesturePhaseMove, Point: pt}, up}}}}, true},
		{"extra down", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{down, down, up}}}}, true},
		{"decreasing offset", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{{Phase: GesturePhaseDown, Point: pt, AtOffsetSeconds: 1}, up}}}}, true},
		{"negative coordinate", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{down, {Phase: GesturePhaseUp, Point: wire.Point{X: -1}, AtOffsetSeconds: 0.1}}}}}, true},
		{"pressure out of range", Gesture{Pointers: []GesturePointer{{ID: "a", Waypoints: []GestureWaypoint{{Phase: GesturePhaseDown, Point: pt, Pressure: &bad}, up}}}}, true},
	}
	for _, c := range cases {
		err := ValidateGesture(c.g)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
			continue
		}
		if err != nil {
			if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
				t.Errorf("%s: code = %q, want %q", c.name, code, diag.CodeInvalidArgument)
			}
		}
	}
}
