package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

const (
	// defaultHandleTimeoutMs is handle's poll budget when the step
	// carries no explicit timeout.
	defaultHandleTimeoutMs = 5_000
	// handlePollIntervalMs is the delay between successive branch
	// re-evaluations while handle waits for one to match.
	handlePollIntervalMs = 100
)

// HandleCommand polls the current screen state and runs the first
// matching branch's steps; Otherwise ("fail" or "skip") applies when
// no branch matches within the timeout.
type HandleCommand struct{}

func (HandleCommand) Command() string { return script.CommandHandle }

func (HandleCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.HandleStep)
	timeoutMs := defaultHandleTimeoutMs
	if s.Timeout != nil {
		timeoutMs = *s.Timeout
	}
	deadline := ctx.Clock.NowMs() + int64(timeoutMs)
	parentEnv := environmentFromVariables(ctx.Variables)

	for {
		for _, branch := range s.Branches {
			if handleBranchMatches(branch.When, ctx) {
				steps, errDetail := resolveStepsOrFile(ctx.FlowLoader, branch.Do, parentEnv)
				if errDetail != nil {
					return CommandResult{Ok: false, Detail: errDetail}
				}
				return ctx.RunSteps(steps)
			}
		}
		if ctx.Clock.NowMs() >= deadline {
			break
		}
		must0(ctx.Clock.Sleep(ctx.Context, handlePollIntervalMs))
	}

	if s.Otherwise != nil && *s.Otherwise == "skip" {
		return CommandResult{Ok: true, Detail: strPtr("handle: no branch matched, skipped")}
	}
	return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
		"handle: no branch matched within %dms (otherwise: fail)", timeoutMs))}
}

// handleBranchMatches reports whether when's visible/notVisible
// conditions currently hold against the live accessibility tree.
func handleBranchMatches(when script.HandleCondition, ctx *CommandContext) bool {
	if when.Visible != nil {
		selector := compileScriptSelector(toScriptSelector(*when.Visible))
		if must(ctx.Orchestra.FindOne(ctx.Context, selector)) == nil {
			return false
		}
	}
	if when.NotVisible != nil {
		selector := compileScriptSelector(toScriptSelector(*when.NotVisible))
		if must(ctx.Orchestra.FindOne(ctx.Context, selector)) != nil {
			return false
		}
	}
	return true
}
