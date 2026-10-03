package runner

import (
	"errors"
	"fmt"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// AssertExprCommand asserts that a boolean expression over the run's
// variables evaluates to true. It is the only command that uses the
// expression engine.
type AssertExprCommand struct{}

func (AssertExprCommand) Command() string { return script.CommandAssertExpr }

func (AssertExprCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.AssertExprStep)
	written := authored[*script.AssertExprStep](step).Expr

	result, err := expression.Evaluate(s.Expr, NewVariablesLookup(ctx.Variables))
	if err != nil {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"Cannot evaluate assertion \"%s\": %s", written, evaluationErrorText(err, s.Expr != written)))}
	}

	if result.Kind() != expression.KindBool {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"Assertion \"%s\" must evaluate to a boolean, got %s",
			written, jsTypeOfValue(result)))}
	}

	if !result.Bool() {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Assertion failed: \"%s\" did not hold", written))}
	}

	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Assertion passed: \"%s\"", written))}
}

// evaluationErrorText renders an expression failure for a step detail. A
// syntax error names the offending token of the executed text, which holds
// the values `${var}` placeholders expanded to, so when the author wrote
// placeholders the detail names only the position.
func evaluationErrorText(err error, hasPlaceholders bool) string {
	var syntax *expression.ParseError
	if hasPlaceholders && errors.As(err, &syntax) {
		return fmt.Sprintf("the expression has a syntax error at position %d", syntax.Position)
	}
	return err.Error()
}
