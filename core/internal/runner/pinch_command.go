package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

// pinchDefaultAxis places the two contact points along the vertical
// axis when the step names none.
const pinchDefaultAxis = "vertical"

// PinchCommand performs a two-finger pinch centered on a target,
// moving the contact points from one gap to another.
type PinchCommand struct{}

func (PinchCommand) Command() string { return script.CommandPinch }

func (PinchCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	return gestureSugarResult(executePinch(step.(*script.PinchStep), ctx))
}

func executePinch(s *script.PinchStep, ctx *CommandContext) (CommandResult, error) {
	center, err := resolvePointerTarget(s.Center, ctx)
	if err != nil {
		return CommandResult{}, err
	}
	axis := pinchDefaultAxis
	if s.Axis != nil {
		axis = *s.Axis
	}
	durationMs := float64(pointerDefaultMoveDurationMs)
	if s.DurationMs != nil {
		durationMs = *s.DurationMs
	}

	// The two contacts sit half a gap out from the center on opposite
	// sides along the axis, and move to half the end gap. A larger end
	// gap spreads them apart (a zoom-in).
	var ux, uy float64
	if axis == "horizontal" {
		ux = 1
	} else {
		uy = 1
	}
	startOffset := s.StartGap / 2
	endOffset := s.EndGap / 2

	touches := []script.PointerTouch{
		gapTouch("pinch-1", center, ux*startOffset, uy*startOffset, ux*endOffset, uy*endOffset),
		gapTouch("pinch-2", center, -ux*startOffset, -uy*startOffset, -ux*endOffset, -uy*endOffset),
	}

	if _, err := dispatchMultiTouch(touches, durationMs, ctx); err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("pinch %s (gap %g to %g)", axis, s.StartGap, s.EndGap))}, nil
}

// gapTouch builds one pinch contact: down at the center offset by
// (startDx, startDy), a move to the center offset by (endDx, endDy),
// then up.
func gapTouch(id string, center wire.Point, startDx, startDy, endDx, endDy float64) script.PointerTouch {
	return script.PointerTouch{
		ID: id,
		Actions: []script.PointerAction{
			pointTargetAction("down", wire.Point{X: center.X + startDx, Y: center.Y + startDy}),
			pointTargetAction("move", wire.Point{X: center.X + endDx, Y: center.Y + endDy}),
			{Type: "up"},
		},
	}
}
