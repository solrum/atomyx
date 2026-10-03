package script

import "gopkg.in/yaml.v3"

// ParseScenario parses a YAML scenario document into a validated
// ScenarioDefinition, where the YAML
// is decoded, its root is checked to be a mapping, and every field is
// checked against the rules ScenarioDefinitionSchema enforces at
// runtime. It returns a non-nil error for malformed YAML, a
// non-mapping root, or any schema violation; a schema violation's
// error text is the same "Scenario validation failed:" listing
// of every issue.
func ParseScenario(yamlContent string) (*ScenarioDefinition, error) {
	var raw any
	if err := yaml.Unmarshal([]byte(yamlContent), &raw); err != nil {
		return nil, newParseError("Invalid YAML in scenario: %s", err.Error())
	}

	root, ok := raw.(map[string]any)
	if !ok {
		return nil, newParseError("Scenario root must be a YAML mapping with `name` and `scripts` keys")
	}

	def, issues := validateScenario(root)
	if len(issues) > 0 {
		return nil, newParseError("Scenario validation failed:\n%s", formatZodIssues(issues))
	}
	return def, nil
}

// IsScenarioYaml is the cheap shape-detection ParseScenario's callers
// use to decide between ParseScript and ParseScenario for an untyped
// YAML string: true when the parsed root is a mapping whose `scripts`
// key holds a YAML sequence, the structural fingerprint of a
// scenario document. Tolerant of a parse failure: returns false
// rather than propagating the error.
func IsScenarioYaml(yamlContent string) bool {
	var raw any
	if err := yaml.Unmarshal([]byte(yamlContent), &raw); err != nil {
		return false
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	_, ok = root["scripts"].([]any)
	return ok
}
