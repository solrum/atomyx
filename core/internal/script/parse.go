package script

import "strings"

// ParseScript parses a YAML test script string into a validated
// ScriptDefinition, mirroring TypeScript's parseScript: YAML
// shorthand is expanded into canonical steps, `${var}` placeholders
// are resolved against externalEnv merged over the script's own
// `env:` block (externalEnv wins on key collision), and the result is
// checked against every schema rule. It returns a non-nil error for
// any structural or validation failure; externalEnv may be nil.
func ParseScript(yamlContent string, externalEnv map[string]string) (*ScriptDefinition, error) {
	docs, err := splitScriptDocs(yamlContent)
	if err != nil {
		return nil, err
	}

	scriptEnv := coerceStringRecord(docs.config["env"])
	mergedEnv := make(map[string]string, len(scriptEnv)+len(externalEnv))
	for k, v := range scriptEnv {
		mergedEnv[k] = v
	}
	for k, v := range externalEnv {
		mergedEnv[k] = v
	}

	resolvedStepsAny := resolveVariables(any(docs.rawSteps), mergedEnv)
	resolvedSteps, _ := resolvedStepsAny.([]any)

	appIDRaw, appIDPresent := docs.config["appId"]
	if !appIDPresent || appIDRaw == nil {
		appIDRaw, appIDPresent = docs.config["app"]
	}
	appIDStr := ""
	if appIDPresent && appIDRaw != nil {
		s, ok := asString(appIDRaw)
		if !ok {
			return nil, newParseError("appId: expected a string")
		}
		appIDStr = s
	}
	appID, _ := resolveVariables(appIDStr, mergedEnv).(string)

	steps := make([]ScriptStep, 0, len(resolvedSteps))
	for i, raw := range resolvedSteps {
		step, err := normalizeStep(raw, i)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	if len(steps) == 0 && appID == "" {
		return nil, newParseError("Script has no steps")
	}

	def := &ScriptDefinition{
		AppID: appID,
		Env:   mergedEnv,
		Steps: steps,
	}

	if err := applyConfigFields(def, docs.config); err != nil {
		return nil, err
	}

	def.StepLines = padStepLines(docs.stepLines, len(steps))

	if errs := Validate(def); len(errs) > 0 {
		return nil, newParseError("Invalid script:\n  %s", strings.Join(errs, "\n  "))
	}

	return def, nil
}

// applyConfigFields copies every remaining ScriptDefinition field
// (beyond appId, env, and steps, already set by the caller) off the
// raw config mapping, applying the format and name defaults and
// rejecting a field whose YAML type does not match its schema type.
func applyConfigFields(def *ScriptDefinition, config map[string]any) error {
	if v, ok := config["format"]; ok && v != nil {
		s, ok := asString(v)
		if !ok {
			return newParseError("format: expected a string")
		}
		def.Format = &s
	} else {
		s := ScriptDefaults.Format
		def.Format = &s
	}

	if v, ok := config["name"]; ok && v != nil {
		s, ok := asString(v)
		if !ok {
			return newParseError("name: expected a string")
		}
		def.Name = s
	} else {
		def.Name = ScriptDefaults.Name
	}

	if v, ok := config["description"]; ok && v != nil {
		s, ok := asString(v)
		if !ok {
			return newParseError("description: expected a string")
		}
		def.Description = &s
	}

	if v, ok := config["precondition"]; ok && v != nil {
		s, ok := asString(v)
		if !ok {
			return newParseError("precondition: expected a string")
		}
		def.Precondition = &s
	}

	if v, ok := config["proxy"]; ok && v != nil {
		s, ok := asString(v)
		if !ok {
			return newParseError("proxy: expected a string")
		}
		def.Proxy = &s
	}

	if v, ok := config["tags"]; ok && v != nil {
		tags, err := decodeStringSlice(v, "tags")
		if err != nil {
			return err
		}
		def.Tags = tags
	}

	if v, ok := config["requires"]; ok && v != nil {
		requires, err := decodeStringSlice(v, "requires")
		if err != nil {
			return err
		}
		def.Requires = requires
	}

	if v, ok := config["stepDelay"]; ok && v != nil {
		f, ok := asFloat(v)
		if !ok {
			return newParseError("stepDelay: expected a number")
		}
		def.StepDelay = &f
	}

	if v, ok := config["preconditions"]; ok && v != nil {
		arr, ok := asSlice(v)
		if !ok {
			return newParseError("preconditions: expected an array")
		}
		preconditions := make([]Precondition, 0, len(arr))
		for _, raw := range arr {
			p, err := normalizePreconditionSetup(raw)
			if err != nil {
				return err
			}
			preconditions = append(preconditions, *p)
		}
		def.Preconditions = preconditions
	}

	return nil
}

func decodeStringSlice(v any, field string) ([]string, error) {
	arr, ok := asSlice(v)
	if !ok {
		return nil, newParseError("%s: expected an array", field)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := asString(item)
		if !ok {
			return nil, newParseError("%s: expected an array of strings", field)
		}
		out = append(out, s)
	}
	return out, nil
}

func padStepLines(lines []int, expected int) []int {
	out := make([]int, expected)
	for i := range out {
		if i < len(lines) {
			out[i] = lines[i]
		}
	}
	return out
}
