package runner

import (
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// SwipeCommand swipes the viewport's centre in the step's direction.
type SwipeCommand struct{}

func (SwipeCommand) Command() string { return script.CommandSwipe }

func (SwipeCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.SwipeStep)
	must0(ctx.Orchestra.SwipeDirection(ctx.Context, orchestra.Direction(s.Direction), orchestra.SwipeDirectionOptions{}))
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Swiped %s", s.Direction))}
}
