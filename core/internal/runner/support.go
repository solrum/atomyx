package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// authored returns step as the script author wrote it, as the same step
// kind, so a detail can name the author's text where the executed step
// holds the value a `${var}` placeholder expanded to.
func authored[T script.ScriptStep](step script.ScriptStep) T {
	return script.AuthoredOf(step).(T)
}

// withheldResult replaces the detail of a step whose written form is
// lost with the command name and the outcome, because every field the
// step holds may be a value a variable supplied, and so may any detail a
// handler built from them.
func withheldResult(step script.ScriptStep, result CommandResult) CommandResult {
	outcome := "failed"
	if result.Ok {
		outcome = "completed"
	}
	result.Detail = strPtr(step.StepCommand() + " " + outcome)
	return result
}

// restoreAuthored puts the author's text back into text that a component
// outside the runner built from the executed value, so the value a
// variable supplied does not reach a step detail through that component.
func restoreAuthored(text, executed, written string) string {
	if executed == "" || executed == written {
		return text
	}
	return strings.ReplaceAll(text, executed, written)
}

// detailOrEmpty renders a CommandResult.Detail pointer as a plain
// string, treating nil as empty. Command handlers that fold a nested
// CommandResult's detail into their own failure message use this
// instead of repeating the nil check inline.
func detailOrEmpty(d *string) string {
	if d == nil {
		return ""
	}
	return *d
}

// must panics with err when it is non-nil, otherwise returns value.
// A command handler calling an Orchestra, Clock, or driver method
// uses this to propagate a failure without threading an error return
// through every call site: CommandDefinition.Execute has no error
// return, so the runner's dispatch recovers the panic into a failed
// CommandResult carrying an "Exception: " prefixed detail.
func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

// must0 is must for a call that returns only an error.
func must0(err error) {
	if err != nil {
		panic(err)
	}
}

// commandFailureFor converts err into an ordinary failed CommandResult
// carrying its diag message, for use in place of must or must0 at a
// call site whose error can be a known, expected outcome rather than a
// bug in the handler: an app that is not installed, an app id that
// fails format validation, a field that never takes input focus. ok is
// false when err carries no stable diag code, so the caller falls back
// to must or must0 and lets the deferred recover treat the failure as
// unexpected.
func commandFailureFor(err error) (CommandResult, bool) {
	var coded diag.Coded
	if !errors.As(err, &coded) {
		return CommandResult{}, false
	}
	return CommandResult{Ok: false, Detail: strPtr(coded.Error()), Code: coded.Code()}, true
}

// obscuredDetail is the step detail for an action refused because
// another element covers its target. The orchestra reason names the
// covering element's label, which is screen text, so a step record says
// only that the target is covered.
const obscuredDetail = "element is visually obscured by another element"

// actionFailureDetail returns the step detail for a failed action.
func actionFailureDetail(result orchestra.ActionResult) string {
	if result.Obscurer != nil {
		return obscuredDetail
	}
	return result.Reason
}

// jsTypeOfValue renders an expression.Value's Kind the way
// JavaScript's `typeof` operator would, matching assertExpr's
// non-boolean failure message wording.
func jsTypeOfValue(v expression.Value) string {
	switch v.Kind() {
	case expression.KindNumber:
		return "number"
	case expression.KindString:
		return "string"
	case expression.KindBool:
		return "boolean"
	default:
		return "undefined"
	}
}

// describeCapturedValue renders v for a step detail without printing a
// captured string or composite: a string reports its length in
// characters, an array its item count, and an object its member
// count. A number, a boolean, null and undefined print as is, because
// they carry little that could be a credential and a failed numeric
// comparison is unreadable without them. The detail flows into run
// logs, results, failure bundles and reports, and a captured response
// can hold tokens and personal data.
func describeCapturedValue(v any) string {
	if v == nil {
		return "null"
	}
	if isJSUndefined(v) {
		return "undefined"
	}
	switch t := v.(type) {
	case string:
		return fmt.Sprintf("a string of %d characters", utf8.RuneCountInString(t))
	case []any:
		return fmt.Sprintf("an array of %d items", len(t))
	case map[string]any:
		return fmt.Sprintf("an object with %d members", len(t))
	case bool:
		return "a boolean"
	case float64, int, int64, json.Number:
		return "a number"
	default:
		return "a value of an unreadable type"
	}
}
