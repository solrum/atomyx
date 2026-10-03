package runner

import (
	"fmt"
	"unicode/utf8"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// TypeCommand types text into a specific field (when the step names
// one via Into) or into whatever field currently has focus.
type TypeCommand struct{}

func (TypeCommand) Command() string { return script.CommandType }

func (TypeCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.TypeStep)

	if s.Into != nil {
		selector := compileScriptSelector(*s.Into)
		result, err := ctx.Orchestra.InputText(ctx.Context, selector, s.Text, orchestra.InputTextOptions{})
		if err != nil {
			// The target field never taking input focus in time is a
			// normal, expected outcome (a slow-loading screen, a field
			// that genuinely never becomes focusable), not a bug in this
			// handler, so it fails the step like any other failing
			// command instead of propagating as a panic.
			if failed, ok := commandFailureFor(err); ok {
				return failed
			}
			panic(err)
		}
		if !result.Ok {
			return CommandResult{Ok: false, Detail: strPtr(actionFailureDetail(result))}
		}
		return CommandResult{Ok: true, Detail: strPtr(typedDetail(s.Text, ""))}
	}

	if err := ctx.Orchestra.TypeText(ctx.Context, s.Text); err != nil {
		if failed, ok := commandFailureFor(err); ok {
			return failed
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr(typedDetail(s.Text, " into focused field"))}
}

// typedDetail reports how many characters a type step entered and never
// the text itself, because the text can be a secret and the detail
// flows into run logs, results, failure bundles and reports.
func typedDetail(text, suffix string) string {
	return fmt.Sprintf("Typed %d characters%s", utf8.RuneCountInString(text), suffix)
}
