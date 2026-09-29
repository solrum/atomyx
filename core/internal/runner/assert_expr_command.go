package runner

import (
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

	result, err := expression.Evaluate(s.Expr, NewVariablesLookup(ctx.Variables))
	if err != nil {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"Cannot evaluate assertion \"%s\": %s", s.Expr, err.Error()))}
	}

	if result.Kind() != expression.KindBool {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"Assertion \"%s\" must evaluate to a boolean, got %s (%s)",
			s.Expr, jsTypeOfValue(result), jsonStringifyExprValue(result)))}
	}

	if !result.Bool() {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Assertion failed: \"%s\" did not hold", s.Expr))}
	}

	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Assertion passed: \"%s\"", s.Expr))}
}
