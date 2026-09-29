package runner

import (
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// LongPressCommand presses and holds at a target, then releases; when
// the step names a second target it drags there before releasing.
type LongPressCommand struct{}

func (LongPressCommand) Command() string { return script.CommandLongPress }

func (LongPressCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	return gestureSugarResult(executeLongPress(step.(*script.LongPressStep), ctx))
}

func executeLongPress(s *script.LongPressStep, ctx *CommandContext) (CommandResult, error) {
	holdMs := float64(orchestra.DefaultLongPressDurationMs)
	if s.HoldMs != nil {
		holdMs = *s.HoldMs
	}

	// A single touch: hold at `at`, then optionally drag to
	// `thenSwipeTo` before releasing. The location stays a target union
	// (a coordinate or a selector), which the driver resolves at
	// dispatch time. With a drag the sequence is a hold-then-drag, which
	// the single-touch path routes through the waypoint gesture so the
	// hold and the drag stay distinct on every platform.
	actions := []script.PointerAction{
		{Type: "down", Target: s.At},
		{Type: "wait", Ms: &holdMs},
	}
	detail := fmt.Sprintf("longPress (hold %gms)", holdMs)
	if s.ThenSwipeTo != nil {
		actions = append(actions, script.PointerAction{Type: "move", Target: s.ThenSwipeTo})
		detail = fmt.Sprintf("longPress (hold %gms, then drag)", holdMs)
	}
	actions = append(actions, script.PointerAction{Type: "up"})

	if _, err := dispatchSingleTouch(actions, float64(pointerDefaultMoveDurationMs), ctx); err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Ok: true, Detail: strPtr(detail)}, nil
}
