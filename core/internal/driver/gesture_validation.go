package driver

import (
	"fmt"

	"dev.atomyx/internal/diag"
)

// ValidateGesture checks the shape rules every platform runner
// enforces, so an implementation can reject a malformed gesture
// before it reaches a device. It returns nil for a well-formed
// gesture, otherwise an error carrying diag.CodeInvalidArgument whose
// message names the offending pointer.
//
// A well-formed gesture satisfies all of these:
//
//   - It has at least one pointer.
//   - Every pointer has at least two waypoints.
//   - Every pointer's first waypoint is GesturePhaseDown and its last
//     is GesturePhaseUp.
//   - No waypoint after the first is GesturePhaseDown.
//   - Waypoint offsets never decrease within a pointer.
//   - Waypoint coordinates are non-negative.
//   - A waypoint pressure, when present, lies within [0, 1].
//
// Capability limits (multi-pointer, pressure support) are a separate
// check that depends on the implementation's Capabilities.
func ValidateGesture(gesture Gesture) error {
	if len(gesture.Pointers) == 0 {
		return invalidGesture("the gesture has no pointers")
	}
	for _, p := range gesture.Pointers {
		if err := validatePointer(p); err != nil {
			return err
		}
	}
	return nil
}

func validatePointer(p GesturePointer) error {
	if len(p.Waypoints) < 2 {
		return invalidGesture("pointer '%s' must have at least 2 waypoints (down and up), got %d", p.ID, len(p.Waypoints))
	}
	if first := p.Waypoints[0].Phase; first != GesturePhaseDown {
		return invalidGesture("pointer '%s' must start with phase down, got '%s'", p.ID, first)
	}
	if last := p.Waypoints[len(p.Waypoints)-1].Phase; last != GesturePhaseUp {
		return invalidGesture("pointer '%s' must end with phase up, got '%s'", p.ID, last)
	}
	prev := p.Waypoints[0].AtOffsetSeconds
	for i, w := range p.Waypoints {
		if i > 0 && w.Phase == GesturePhaseDown {
			return invalidGesture("pointer '%s' has an extra down waypoint at index %d", p.ID, i)
		}
		if w.AtOffsetSeconds < prev {
			return invalidGesture("pointer '%s' waypoint offsets must not decrease (got %gs after %gs)", p.ID, w.AtOffsetSeconds, prev)
		}
		prev = w.AtOffsetSeconds
		if w.Point.X < 0 || w.Point.Y < 0 {
			return invalidGesture("pointer '%s' coordinates must be non-negative (got %g, %g)", p.ID, w.Point.X, w.Point.Y)
		}
		if w.Pressure != nil && (*w.Pressure < 0 || *w.Pressure > 1) {
			return invalidGesture("pointer '%s' pressure %g is outside [0, 1]", p.ID, *w.Pressure)
		}
	}
	return nil
}

func invalidGesture(format string, args ...any) error {
	return diag.New(diag.CodeInvalidArgument, fmt.Sprintf(format, args...), diag.InvalidArgumentRemedy, nil)
}
