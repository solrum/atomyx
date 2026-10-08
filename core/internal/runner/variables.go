package runner

import "dev.atomyx/internal/script/expression"

// ScriptValue is a runtime script variable value: a number, a string,
// or a boolean. It is a direct alias for the expression package's
// Value type, which already models exactly these three variants, so
// NewVariablesLookup can hand a variables map straight to the
// expression evaluator with no conversion step.
type ScriptValue = expression.Value

// CapturedRequest is one HTTP request/response pair recorded by a
// NetworkCapture adapter and bound to a script variable by a
// `capture` step.
type CapturedRequest struct {
	// Method is the HTTP method (GET, POST, PUT, etc.).
	Method string
	// URL is the full request URL.
	URL string
	// Status is the HTTP response status code.
	Status int
	// Headers are the response headers.
	Headers map[string]string
	// Body is the parsed response body: a JSON value decoded onto
	// Go's any (map[string]any, []any, string, float64, bool), or nil
	// when the response body could not be parsed as JSON.
	Body any
	// TimestampMs is the Unix timestamp, in milliseconds, when the
	// response was captured.
	TimestampMs int64
}

// NewVariablesLookup adapts variables into an expression.Lookup, so
// assertExpr and any other expression-evaluating command can resolve
// identifiers against the run's live variable set.
func NewVariablesLookup(variables map[string]ScriptValue) expression.Lookup {
	return func(name string) (expression.Value, bool) {
		v, ok := variables[name]
		return v, ok
	}
}

// variablesFromEnv seeds a run's variables map from a script's env
// block. Every env value is a string, so each becomes a KindString
// ScriptValue.
func variablesFromEnv(env map[string]string) map[string]ScriptValue {
	variables := make(map[string]ScriptValue, len(env))
	for k, v := range env {
		variables[k] = expression.StringValue(v)
	}
	return variables
}

// secretsFromEnv tags every env entry as secret. The env map merges the
// script's own env block, the config environment's vars (including
// resolved ${ENV:...} references) and the caller's external env, and
// the merge no longer says which entry came from where, so the whole
// map is treated as secret rather than risking a leak.
func secretsFromEnv(env map[string]string) map[string]bool {
	secrets := make(map[string]bool, len(env))
	for k := range env {
		secrets[k] = true
	}
	return secrets
}

// environmentFromVariables renders variables to the string form
// `${var}` interpolation needs: a string passes through unchanged, a
// number renders via the same decimal form the expression engine's
// `string()` cast uses, and a boolean renders as "true" or "false".
// Every command that resolves a `handle`/`branch` `do` file, a
// `runFlow` target, a `requires` entry, or a precondition `setup`
// file calls this to build the external environment the loaded
// flow's own `${var}` placeholders resolve against.
func environmentFromVariables(variables map[string]ScriptValue) map[string]string {
	env := make(map[string]string, len(variables))
	for k, v := range variables {
		env[k] = scriptValueToEnvString(v)
	}
	return env
}

// scriptValueToEnvString renders one ScriptValue the way JavaScript's
// String(value) renders the equivalent number/string/boolean.
func scriptValueToEnvString(v ScriptValue) string {
	switch v.Kind() {
	case expression.KindNumber:
		return expression.FormatNumber(v.Num())
	case expression.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	default: // KindString
		return v.Str()
	}
}
