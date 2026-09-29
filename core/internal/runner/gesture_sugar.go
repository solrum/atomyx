package runner

import (
	"errors"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

// High-level gesture-sugar steps (longPress, pinch, rotate) compile to
// the same pointer gesture path the raw `pointer` step uses, through
// the dispatchSingleTouch and dispatchMultiTouch helpers. The shared
// pieces below are what all three reuse.

// gestureSugarResult maps a gesture-sugar step's dispatch outcome to a
// CommandResult: a pointerError (a validation, capability, or
// selector-resolution failure the step can attribute to its own input)
// becomes a failed step, and any other error propagates as a panic the
// runner recovers into an "Exception:" step, matching the raw pointer
// command's own failure contract.
func gestureSugarResult(result CommandResult, err error) CommandResult {
	if err != nil {
		var perr *pointerError
		if errors.As(err, &perr) {
			return CommandResult{Ok: false, Detail: strPtr(perr.Error())}
		}
		panic(err)
	}
	return result
}

// pointTargetAction builds a pointer action of the given phase whose
// target is the absolute coordinate p. The gesture-sugar steps that
// resolve a center to a coordinate (pinch, rotate) place their contact
// points with it.
func pointTargetAction(actionType string, p wire.Point) script.PointerAction {
	return script.PointerAction{Type: actionType, Target: &script.PointerTarget{Point: &script.Point{X: p.X, Y: p.Y}}}
}
