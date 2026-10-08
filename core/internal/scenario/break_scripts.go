package scenario

import (
	"path/filepath"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// mergeChildEnv returns the variables every child script parses with:
// the scenario's own env, overridden per key by the host's.
func mergeChildEnv(scenarioEnv, external map[string]string) map[string]string {
	merged := make(map[string]string, len(scenarioEnv)+len(external))
	for k, v := range scenarioEnv {
		merged[k] = v
	}
	for k, v := range external {
		merged[k] = v
	}
	return merged
}

// childSourcePath is the absolute path of a child script named relative
// to dir, or empty when the scenario has no directory.
func childSourcePath(dir, scriptPath string) string {
	if dir == "" {
		return ""
	}
	if filepath.IsAbs(scriptPath) {
		return filepath.Clean(scriptPath)
	}
	abs, err := filepath.Abs(filepath.Join(dir, scriptPath))
	if err != nil {
		return ""
	}
	return abs
}

// BreakScripts loads and parses every child script of def exactly as a
// run would, so break locations can be validated before a device is
// taken. dir is the same directory ScenarioRunnerDeps.SourceDir names.
func BreakScripts(def *script.ScenarioDefinition, externalEnv map[string]string, load LoadScript, dir string) ([]runner.BreakScript, error) {
	env := mergeChildEnv(def.Env, externalEnv)
	out := make([]runner.BreakScript, 0, len(def.Scripts))
	for _, scriptPath := range def.Scripts {
		yaml, err := load(scriptPath)
		if err != nil {
			return nil, err
		}
		parsed, err := script.ParseScript(yaml, env)
		if err != nil {
			return nil, err
		}
		out = append(out, runner.BreakScript{Path: childSourcePath(dir, scriptPath), Definition: parsed})
	}
	return out, nil
}
