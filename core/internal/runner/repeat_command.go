package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// RepeatCommand runs its nested steps in a loop. A `times` count bounds
// the loop from above, a `while` condition is checked before every
// iteration, and a loop with a condition and no count fails once it has
// run script.RepeatWhileIterationLimit iterations with the condition
// still holding. The first failing nested step ends the loop and the
// failure names the iteration it happened in.
type RepeatCommand struct{}

func (RepeatCommand) Command() string { return script.CommandRepeat }

func (RepeatCommand) runsNestedSteps() {}

func (RepeatCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.RepeatStep)
	written := authored[*script.RepeatStep](step)

	iterations := 0
	for {
		must0(ctx.Context.Err())

		if s.Times != nil && iterations >= *s.Times {
			return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
				"repeat: completed %d %s", iterations, pluralWord(iterations, "iteration")))}
		}

		if s.While != nil {
			holds, failure := repeatConditionHolds(s.While, written.While, ctx)
			if failure != nil {
				return *failure
			}
			if !holds {
				return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
					"repeat: condition ended the loop after %d %s", iterations, pluralWord(iterations, "iteration")))}
			}
			if s.Times == nil && iterations >= script.RepeatWhileIterationLimit {
				return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
					"repeat: condition still holds after %d iterations, which is the limit when `times` is not set",
					script.RepeatWhileIterationLimit))}
			}
		}

		iterations++
		result := ctx.RunSteps(s.Commands)
		if !result.Ok {
			must0(ctx.Context.Err())
			return CommandResult{Ok: false, Code: result.Code, Detail: strPtr(fmt.Sprintf(
				"repeat: %s failed: %s", iterationLabel(iterations, s.Times), detailOrEmpty(result.Detail)))}
		}
	}
}

// iterationLabel names iteration n, with the total when a count bounds
// the loop.
func iterationLabel(n int, times *int) string {
	if times == nil {
		return fmt.Sprintf("iteration %d", n)
	}
	return fmt.Sprintf("iteration %d of %d", n, *times)
}

// repeatConditionHolds evaluates cond against the live screen or the
// run's variables. A non-nil failure is an authoring error in the
// condition itself, such as an expression that does not evaluate to a
// boolean. written is the condition as the author wrote it, so a detail
// never carries a value a `${var}` placeholder expanded to.
func repeatConditionHolds(cond, written *script.RepeatCondition, ctx *CommandContext) (bool, *CommandResult) {
	switch {
	case cond.Visible != nil:
		selector := compileScriptSelector(toScriptSelector(*cond.Visible))
		return must(ctx.Orchestra.FindOne(ctx.Context, selector)) != nil, nil
	case cond.NotVisible != nil:
		selector := compileScriptSelector(toScriptSelector(*cond.NotVisible))
		return must(ctx.Orchestra.FindOne(ctx.Context, selector)) == nil, nil
	case cond.Expr != nil:
		return repeatExprHolds(*cond.Expr, *written.Expr, ctx)
	}
	return false, &CommandResult{Ok: false, Detail: strPtr("repeat: the `while` condition sets none of visible, notVisible or expr")}
}

func repeatExprHolds(expr, writtenExpr string, ctx *CommandContext) (bool, *CommandResult) {
	value, err := expression.Evaluate(expr, NewVariablesLookup(ctx.Variables))
	if err != nil {
		return false, &CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"repeat: cannot evaluate condition \"%s\": %s", writtenExpr, evaluationErrorText(err, expr != writtenExpr)))}
	}
	if value.Kind() != expression.KindBool {
		return false, &CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"repeat: condition \"%s\" must evaluate to a boolean, got %s", writtenExpr, jsTypeOfValue(value)))}
	}
	return value.Bool(), nil
}

func pluralWord(count int, word string) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
