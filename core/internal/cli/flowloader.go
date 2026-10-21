package cli

import (
	"os"
	"path/filepath"

	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/script"
)

// fsFlowLoader resolves a runner.FlowLoader request by reading path
// off the local filesystem and parsing it as a script. It performs no
// caching between calls, so every load reflects the file's current
// contents on disk, whether the request is for a required flow, a
// precondition setup file, or a handle or branch do file.
type fsFlowLoader struct{}

// LoadFlow implements runner.FlowLoader.
func (fsFlowLoader) LoadFlow(path string, env map[string]string) (*script.ScriptDefinition, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return script.ParseScript(string(content), env, script.WithoutRunTimeResolution())
}

// fsScenarioScriptLoader resolves a scenario.LoadScript request by
// reading a child script's YAML off the local filesystem, relative to
// dir. Resolving relative to the scenario file's own directory rather
// than the process's working directory lets a scenario's `scripts`
// entries stay valid regardless of where the command is invoked from.
func fsScenarioScriptLoader(dir string) scenario.LoadScript {
	return func(rel string) (string, error) {
		content, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		return string(content), nil
	}
}
