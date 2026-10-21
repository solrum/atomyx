package runner

import (
	"dev.atomyx/internal/script"
)

// resolveForRun returns step with the run variables in rs written into
// the text of its eligible fields, ready to gate, summarize and execute.
// A step with nothing to resolve comes back as it is. An error names the
// step, the field and the placeholder, never a value.
func resolveForRun(rs *runState, step script.ScriptStep) (script.ScriptStep, error) {
	if !script.NeedsResolution(step) {
		return step, nil
	}
	resolved, _, err := script.ResolveStep(step, func(name string) (string, bool) {
		value, ok := rs.variables[name]
		if !ok {
			return "", false
		}
		return scriptValueToEnvString(value), true
	})
	if err != nil {
		return step, err
	}
	return resolved, nil
}
