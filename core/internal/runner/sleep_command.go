package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// SleepCommand unconditionally waits the step's duration.
type SleepCommand struct{}

func (SleepCommand) Command() string { return script.CommandSleep }

func (SleepCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.SleepStep)
	must0(ctx.Clock.Sleep(ctx.Context, int64(s.Ms)))
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Slept %dms", s.Ms))}
}
