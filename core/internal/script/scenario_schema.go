package script

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// scenarioFormatRegex mirrors the regex ScenarioFormatSchema validates
// against: "atomyx-scenario/v<n>".
var scenarioFormatRegex = regexp.MustCompile(`^atomyx-scenario/v\d+$`)

// scenarioKnownKeys is every key ScenarioDefinitionSchema declares.
// ScenarioDefinitionSchema is `.strict()`, so any other key on a
// scenario's root mapping is a validation error.
var scenarioKnownKeys = map[string]struct{}{
	"format":        {},
	"name":          {},
	"description":   {},
	"scripts":       {},
	"onFailure":     {},
	"preconditions": {},
	"env":           {},
	"tags":          {},
}

// ScenarioDefaults are the fallbacks a scenario consumer applies for a
// field ScenarioDefinitionSchema leaves unset. Neither the schema nor
// ParseScenario applies them: `format` and `onFailure` carry no zod
// `.default(...)`, so a scenario that omits them parses with those
// fields absent, exactly as TypeScript's parseScenario does. A caller
// that runs a parsed scenario is the one that substitutes these
// values.
var ScenarioDefaults = struct {
	Format    string
	OnFailure string
}{
	Format:    "atomyx-scenario/v1",
	OnFailure: "stop",
}

// ScenarioDefinition is a parsed and validated Atomyx YAML scenario:
// an ordered group of independent scripts run as one unit, each in
// its own clean execution context (variables, captures, artifacts).
type ScenarioDefinition struct {
	Format        *string           `json:"format,omitempty"`
	Name          string            `json:"name"`
	Description   *string           `json:"description,omitempty"`
	Scripts       []string          `json:"scripts"`
	OnFailure     *string           `json:"onFailure,omitempty"`
	Preconditions *string           `json:"preconditions,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
}

// zodIssue is one field-validation failure, carrying the same path
// and message shape a zod issue does, so formatZodIssues can render
// parseScenario's exact "Scenario validation failed:" text.
type zodIssue struct {
	path    []string
	message string
}

func (i zodIssue) pathString() string {
	if len(i.path) == 0 {
		return "<root>"
	}
	return strings.Join(i.path, ".")
}

// formatZodIssues renders issues the way parseScenario in TypeScript
// renders a caught ZodError: one "  - <path>: <message>" line per
// issue, in encounter order, path defaulting to "<root>" when empty.
func formatZodIssues(issues []zodIssue) string {
	lines := make([]string, len(issues))
	for i, iss := range issues {
		lines[i] = fmt.Sprintf("  - %s: %s", iss.pathString(), iss.message)
	}
	return strings.Join(lines, "\n")
}

// invalidTypeMessage renders zod's default `invalid_type` message: the
// wrong type name Go decoded v as, using the same zod type vocabulary
// zodReceivedType (normalize.go) already renders for a script's
// extract.values issues.
func invalidTypeMessage(expected string, v any) string {
	return fmt.Sprintf("Expected %s, received %s", expected, zodReceivedType(v))
}

func tooSmallStringMessage(min int) string {
	return fmt.Sprintf("String must contain at least %d character(s)", min)
}

func tooSmallArrayMessage(min int) string {
	return fmt.Sprintf("Array must contain at least %d element(s)", min)
}

// joinQuoted renders values the way zod's util.joinValues does: each
// value single-quoted, joined by sep.
func joinQuoted(values []string, sep string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + v + "'"
	}
	return strings.Join(quoted, sep)
}

// invalidEnumTypeMessage renders zod's `invalid_type` message for an
// enum field given a non-string value: zod substitutes the enum's own
// quoted option list, joined by " | ", for the usual expected-type
// name.
func invalidEnumTypeMessage(options []string, v any) string {
	return fmt.Sprintf("Expected %s, received %s", joinQuoted(options, " | "), zodReceivedType(v))
}

// invalidEnumValueMessage renders zod's `invalid_enum_value` message
// for a string value that is not one of options.
func invalidEnumValueMessage(options []string, received string) string {
	return fmt.Sprintf("Invalid enum value. Expected %s, received '%s'", joinQuoted(options, " | "), received)
}

// validateScenario decodes raw, a scenario document's parsed YAML
// root mapping, into a ScenarioDefinition, checking every rule
// ScenarioDefinitionSchema enforces at runtime: required fields, the
// format regex, string and array minimum lengths, enum membership,
// and (last, since it is a whole-object check) `.strict()` unknown-key
// rejection. It returns every issue found, in ScenarioDefinitionSchema's
// own field declaration order; when issues is empty, the returned
// *ScenarioDefinition is fully populated.
func validateScenario(raw map[string]any) (*ScenarioDefinition, []zodIssue) {
	var issues []zodIssue
	report := func(path []string, message string) {
		issues = append(issues, zodIssue{path: path, message: message})
	}

	def := &ScenarioDefinition{}

	if v, present := raw["format"]; present {
		if s, ok := v.(string); ok {
			if scenarioFormatRegex.MatchString(s) {
				def.Format = &s
			} else {
				report([]string{"format"}, "format must match 'atomyx-scenario/v<n>' (e.g. 'atomyx-scenario/v1')")
			}
		} else {
			report([]string{"format"}, invalidTypeMessage("string", v))
		}
	}

	if v, present := raw["name"]; !present {
		report([]string{"name"}, "Required")
	} else if s, ok := v.(string); !ok {
		report([]string{"name"}, invalidTypeMessage("string", v))
	} else if len(s) == 0 {
		report([]string{"name"}, tooSmallStringMessage(1))
	} else {
		def.Name = s
	}

	if v, present := raw["description"]; present {
		if s, ok := v.(string); ok {
			def.Description = &s
		} else {
			report([]string{"description"}, invalidTypeMessage("string", v))
		}
	}

	if v, present := raw["scripts"]; !present {
		report([]string{"scripts"}, "Required")
	} else if arr, ok := v.([]any); !ok {
		report([]string{"scripts"}, invalidTypeMessage("array", v))
	} else {
		if len(arr) == 0 {
			report([]string{"scripts"}, tooSmallArrayMessage(1))
		}
		scripts := make([]string, 0, len(arr))
		for i, item := range arr {
			path := []string{"scripts", strconv.Itoa(i)}
			s, ok := item.(string)
			switch {
			case !ok:
				report(path, invalidTypeMessage("string", item))
			case len(s) == 0:
				report(path, tooSmallStringMessage(1))
			default:
				scripts = append(scripts, s)
			}
		}
		def.Scripts = scripts
	}

	if v, present := raw["onFailure"]; present {
		options := []string{"stop", "continue"}
		if s, ok := v.(string); !ok {
			report([]string{"onFailure"}, invalidEnumTypeMessage(options, v))
		} else if s != "stop" && s != "continue" {
			report([]string{"onFailure"}, invalidEnumValueMessage(options, s))
		} else {
			def.OnFailure = &s
		}
	}

	if v, present := raw["preconditions"]; present {
		options := []string{"skip", "force"}
		if s, ok := v.(string); !ok {
			report([]string{"preconditions"}, invalidEnumTypeMessage(options, v))
		} else if s != "skip" && s != "force" {
			report([]string{"preconditions"}, invalidEnumValueMessage(options, s))
		} else {
			def.Preconditions = &s
		}
	}

	if v, present := raw["env"]; present {
		if m, ok := asMap(v); !ok {
			report([]string{"env"}, invalidTypeMessage("object", v))
		} else {
			env := make(map[string]string, len(m))
			for k, vv := range m {
				path := []string{"env", k}
				if s, ok := vv.(string); ok {
					env[k] = s
				} else {
					report(path, invalidTypeMessage("string", vv))
				}
			}
			def.Env = env
		}
	}

	if v, present := raw["tags"]; present {
		if arr, ok := v.([]any); !ok {
			report([]string{"tags"}, invalidTypeMessage("array", v))
		} else {
			tags := make([]string, 0, len(arr))
			for i, item := range arr {
				path := []string{"tags", strconv.Itoa(i)}
				s, ok := item.(string)
				switch {
				case !ok:
					report(path, invalidTypeMessage("string", item))
				case len(s) == 0:
					report(path, tooSmallStringMessage(1))
				default:
					tags = append(tags, s)
				}
			}
			def.Tags = tags
		}
	}

	var extraKeys []string
	for k := range raw {
		if _, known := scenarioKnownKeys[k]; !known {
			extraKeys = append(extraKeys, k)
		}
	}
	if len(extraKeys) > 0 {
		sort.Strings(extraKeys)
		report(nil, fmt.Sprintf("Unrecognized key(s) in object: %s", joinQuoted(extraKeys, ", ")))
	}

	return def, issues
}
