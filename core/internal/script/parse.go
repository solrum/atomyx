package script

import "strings"

// ParseScript parses a YAML test script string into a validated
// ScriptDefinition, where YAML
// shorthand is expanded into canonical steps, `${var}` placeholders
// are resolved against externalEnv merged over the script's own
// `env:` block (externalEnv wins on key collision), and the result is
// checked against every schema rule. It returns a non-nil error for
// any structural or validation failure; externalEnv may be nil.
//
// A placeholder no env entry names stays literal text, and the leaf step
// holding it can take a run variable of that name later through
// ResolveStep. Preconditions' inline setup steps are decoded as plain
// mappings and keep parse-time-only resolution.
//
// Every externalEnv entry is a secret in the returned definition's
// SecretEnv unless options name it as committed, because a value a host
// supplies comes from outside the script and config files. A value the
// script's own env block declares is never secret, since it is already
// written in the committed file.
func ParseScript(yamlContent string, externalEnv map[string]string, options ...ParseOption) (*ScriptDefinition, error) {
	docs, err := splitScriptDocs(yamlContent)
	if err != nil {
		return nil, err
	}
	var settings parseSettings
	for _, option := range options {
		option(&settings)
	}

	scriptEnv := coerceStringRecord(docs.config["env"])
	mergedEnv := make(map[string]string, len(scriptEnv)+len(externalEnv))
	for k, v := range scriptEnv {
		mergedEnv[k] = v
	}
	secretEnv := make(map[string]bool, len(externalEnv))
	for k, v := range externalEnv {
		mergedEnv[k] = v
		if !settings.committed[k] {
			secretEnv[k] = true
		}
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
	attachAuthoredSteps(steps, docs.rawSteps)
	if !settings.sealed {
		attachPendingSteps(steps, docs.rawSteps, mergedEnv)
	}

	if len(steps) == 0 && appID == "" {
		return nil, newParseError("Script has no steps")
	}

	def := &ScriptDefinition{
		AppID:       appID,
		AppIDSource: appIDStr,
		Env:         mergedEnv,
		SecretEnv:   secretEnv,
		Steps:       steps,
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

// attachAuthoredSteps pairs each resolved step with the same step
// normalized from the script's text before interpolation. When that text
// fails normalization, for example a placeholder where a number belongs,
// writtenForm rebuilds it through stand-in values, and a step whose
// written form still cannot be rebuilt is flagged so it is never shown
// through its expanded fields.
func attachAuthoredSteps(steps []ScriptStep, rawSteps []any) {
	for i, raw := range rawSteps {
		if i >= len(steps) {
			return
		}
		if authored := writtenForm(raw, i); authored != nil {
			attachAuthored(steps[i], authored)
		} else {
			markWrittenFormLost(steps[i])
		}
	}
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

// ParseOption adjusts how ParseScript treats the external env.
type ParseOption func(*parseSettings)

type parseSettings struct {
	committed map[string]bool
	sealed    bool
}

// WithoutRunTimeResolution parses a script whose environment already holds
// every run variable the host has, such as a flow file loaded in the middle
// of a run. A placeholder no name in env resolves stays literal text, and
// no step takes run variables into its text later.
func WithoutRunTimeResolution() ParseOption {
	return func(settings *parseSettings) { settings.sealed = true }
}

// WithCommittedNames names the externalEnv entries whose value is written
// in a committed file, such as a literal in a project config or in a
// scenario, so ParseScript does not count them as secret.
func WithCommittedNames(names map[string]bool) ParseOption {
	return func(settings *parseSettings) { settings.committed = names }
}
