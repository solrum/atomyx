package runner

import (
	"fmt"
	"math"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

const (
	// rotateDefaultRadiusPoints is the center-to-contact distance used
	// when the step names no radius.
	rotateDefaultRadiusPoints = 100
	// rotateArcSegments is how many straight move waypoints approximate
	// each contact's sweep. Splitting the arc keeps each contact on a
	// real curve around the center instead of cutting a chord straight
	// across it.
	rotateArcSegments = 8
)

// RotateCommand performs a two-finger rotation around a target.
type RotateCommand struct{}

func (RotateCommand) Command() string { return script.CommandRotate }

func (RotateCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	return gestureSugarResult(executeRotate(step.(*script.RotateStep), ctx))
}

func executeRotate(s *script.RotateStep, ctx *CommandContext) (CommandResult, error) {
	center, err := resolvePointerTarget(s.Center, ctx)
	if err != nil {
		return CommandResult{}, err
	}
	radius := float64(rotateDefaultRadiusPoints)
	if s.Radius != nil {
		radius = *s.Radius
	}
	durationMs := float64(pointerDefaultMoveDurationMs)
	if s.DurationMs != nil {
		durationMs = *s.DurationMs
	}

	// Two contacts start on opposite sides of the center and sweep the
	// same angle around it, so the gesture reads as a rotation rather
	// than a translation.
	touches := []script.PointerTouch{
		arcTouch("rotate-1", center, radius, 0, s.Degrees),
		arcTouch("rotate-2", center, radius, 180, s.Degrees),
	}

	// Each contact traces its sweep in rotateArcSegments straight moves,
	// so the whole sweep spans durationMs when every move is given an
	// equal slice of it.
	moveDurationMs := durationMs / float64(rotateArcSegments)

	if _, err := dispatchMultiTouch(touches, moveDurationMs, ctx); err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("rotate %g degrees", s.Degrees))}, nil
}

// arcTouch builds one rotation contact: down at startAngleDeg on the
// circle of the given radius around center, then a move to each of
// rotateArcSegments points evenly spaced across the sweep, then up.
func arcTouch(id string, center wire.Point, radius, startAngleDeg, sweepDeg float64) script.PointerTouch {
	actions := make([]script.PointerAction, 0, rotateArcSegments+2)
	actions = append(actions, pointTargetAction("down", arcPoint(center, radius, startAngleDeg)))
	for i := 1; i <= rotateArcSegments; i++ {
		angle := startAngleDeg + sweepDeg*float64(i)/float64(rotateArcSegments)
		actions = append(actions, pointTargetAction("move", arcPoint(center, radius, angle)))
	}
	actions = append(actions, script.PointerAction{Type: "up"})
	return script.PointerTouch{ID: id, Actions: actions}
}

// arcPoint is the point at angleDeg on the circle of the given radius
// around center. The angle grows clockwise on screen, since the y axis
// points down.
func arcPoint(center wire.Point, radius, angleDeg float64) wire.Point {
	rad := angleDeg * math.Pi / 180
	return wire.Point{X: center.X + radius*math.Cos(rad), Y: center.Y + radius*math.Sin(rad)}
}
