package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// BranchCommand routes on the request captured under a name: the
// first matching case's steps run, or the default block when none
// match (a non-match with no default fails the step).
type BranchCommand struct{}

func (BranchCommand) Command() string { return script.CommandBranch }

func (BranchCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.BranchStep)
	fromKey := unwrapFromKey(s.From)
	captured, ok := ctx.Captures[fromKey]
	if !ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"No captured request named \"%s\". Run a \"capture\" step first.", fromKey))}
	}

	parentEnv := environmentFromVariables(ctx.Variables)

	for _, c := range s.On {
		if branchCaseMatches(c.Match, captured) {
			steps, errDetail := resolveStepsOrFile(ctx.FlowLoader, c.Do, parentEnv)
			if errDetail != nil {
				return CommandResult{Ok: false, Detail: errDetail}
			}
			return ctx.RunSteps(steps)
		}
	}

	if s.Default != nil {
		steps, errDetail := resolveStepsOrFile(ctx.FlowLoader, *s.Default, parentEnv)
		if errDetail != nil {
			return CommandResult{Ok: false, Detail: errDetail}
		}
		return ctx.RunSteps(steps)
	}

	return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
		"branch: no case matched for \"%s\" (status=%d)", fromKey, captured.Status))}
}

// branchCaseMatches reports whether captured satisfies match: its
// status (when given) and every body dot-path (when given) must all
// match by exact deep equality. An absent condition places no
// constraint.
func branchCaseMatches(match script.BranchMatchCondition, captured CapturedRequest) bool {
	if match.Status != nil && captured.Status != *match.Status {
		return false
	}
	if match.Body != nil {
		for _, path := range match.Body.Keys() {
			expected, _ := match.Body.Get(path)
			actual := resolveDotPath(captured.Body, path)
			if !jsDeepEqual(actual, expected) {
				return false
			}
		}
	}
	return true
}
