package runner

import (
	"errors"
	"fmt"
	"path/filepath"

	"dev.atomyx/internal/script"
)

// RunFlowCommand loads a sub-flow file, gates it on its own
// preconditions, then runs its steps in this run's shared variables
// and captures, as if the sub-flow's steps were inlined at this
// point in the script.
type RunFlowCommand struct{}

func (RunFlowCommand) Command() string { return script.CommandRunFlow }

func (RunFlowCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.RunFlowStep)
	shownPath := absolutePath(authored[*script.RunFlowStep](step).File)

	// Resolve to an absolute path before using it anywhere, so the
	// "not found" message below always names the same path regardless
	// of the current working directory the run happens to start
	// from.
	filePath := s.File
	if abs, err := filepath.Abs(filePath); err == nil {
		filePath = abs
	}

	if ctx.FlowLoader == nil {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Sub-flow file not found: %s", shownPath))}
	}

	// Merge parent variables with the step's own env overrides. On a
	// key collision, the step's own env value wins over the inherited
	// one.
	mergedEnv := environmentFromVariables(ctx.Variables)
	for k, v := range s.Env {
		mergedEnv[k] = v
	}

	subDef, err := ctx.FlowLoader.LoadFlow(filePath, mergedEnv)
	if err != nil {
		// A parse or schema-validation failure means the sub-flow
		// file's own content is invalid, not that the file could not
		// be read, and that distinction matters for how it surfaces:
		// a *script.ParseError here is panicked rather than folded
		// into "Sub-flow file not found", so the runner's per-step
		// dispatch reports it as an "Exception: " prefixed detail
		// instead of a plain failed-lookup message.
		var parseErr *script.ParseError
		if errors.As(err, &parseErr) {
			panic(err)
		}
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Sub-flow file not found: %s", shownPath))}
	}

	preconditionResult := ctx.RunPreconditions(subDef.Preconditions)
	if !preconditionResult.Ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"Sub-flow \"%s\" precondition failed: %s", subDef.Name, detailOrEmpty(preconditionResult.Detail)))}
	}

	result := ctx.RunSteps(subDef.Steps)
	if !result.Ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"Sub-flow \"%s\" failed: %s", subDef.Name, detailOrEmpty(result.Detail)))}
	}

	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
		"Sub-flow \"%s\" completed (%d steps)", subDef.Name, len(subDef.Steps)))}
}
