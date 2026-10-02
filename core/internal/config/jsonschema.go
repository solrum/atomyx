package config

// GenerateJSONSchema builds a JSON Schema (draft-07) describing every
// document ValidateDocument's field-level rules accept, matching
// AtomyxConfigSchema in atomyx-config.schema.ts field for field:
// required properties, enums, string minLength, minItems, and closed
// objects (every object schema below sets `additionalProperties:
// false`, matching every object in AtomyxConfigSchema, since all of
// them are declared with zod's `.strict()`).
//
// Two rules ValidateDocument enforces are NOT representable here,
// matching the equivalent gap in the current TypeScript schema,
// because zod-to-json-schema drops a schema's `.refine` and
// `.superRefine` callbacks the same way:
//
//   - The top-level rule that every environment's `app` names a
//     declared app id (AtomyxConfigSchema's superRefine).
//   - The rule that a suite entry sets at least one of `tags` or
//     `globs` (SuiteEntrySchema's refine).
//
// A document violating either rule is accepted by this schema and by
// the current TypeScript one alike; ValidateDocument is what rejects
// it at runtime.
func GenerateJSONSchema() map[string]any {
	return map[string]any{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"$id":         "https://schemas.atomyx.dev/config.schema.json",
		"$ref":        "#/definitions/AtomyxConfig",
		"definitions": jsonSchemaDefinitions(),
	}
}

func jsonSchemaDefinitions() map[string]any {
	return map[string]any{
		"AppEntry":         appEntrySchema(),
		"EnvironmentEntry": environmentEntrySchema(),
		"LaunchPolicy":     launchPolicySchema(),
		"Report":           reportSchema(),
		"Session":          sessionSchema(),
		"SuiteEntry":       suiteEntrySchema(),
		"AtomyxConfig":     atomyxConfigSchema(),
	}
}

func appEntrySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":       stringMinLen(1, "Identifier used to reference this app from an environment."),
			"platform": stringEnum([]string{PlatformAndroid, PlatformIOS}, "Target platform: 'android' or 'ios'."),
			"appId":    stringMinLen(1, "Package name (Android) or bundle id (iOS) of the app under test."),
			"artifact": stringSchema("Path to the installable artifact for this app, if any."),
		},
		[]string{"id", "platform", "appId"},
		"One app variant this project can run.",
	)
}

func environmentEntrySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"app":    stringMinLen(1, "Id of the app entry this environment runs."),
			"vars":   stringMapSchema("Variables available to scripts run under this environment."),
			"device": stringMinLen(1, "Exact device identifier for this environment, as reported by list-devices."),
			"proxy":  stringSchema("Proxy configuration reference for this environment."),
		},
		[]string{"app"},
		"One named environment.",
	)
}

func launchPolicySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"mode": stringEnum([]string{LaunchModeFresh, LaunchModeAttach},
				"Default launch mode. 'fresh' force-stops the app before launching a clean process; "+
					"'attach' reuses an already-foreground instance instead of relaunching."),
			"clearData": booleanSchema(
				"When true and mode is 'fresh', reset the app's data to a clean state before launching."),
		},
		nil,
		"Default launch policy applied when a run does not override it.",
	)
}

func reportSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"formats": arraySchema(
				stringEnum([]string{ReportFormatHTML, ReportFormatJSON}, ""), 1,
				"Output formats to produce for a run report.",
			),
			"outDir": stringMinLen(1, "Directory to write reports into."),
		},
		[]string{"formats", "outDir"},
		"Default report output configuration.",
	)
}

func sessionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"mcpIdleTimeoutMs":       integerMin(1, "Milliseconds an MCP session with no tool call keeps its device before releasing it."),
			"openRunIdleTimeoutMs":   integerMin(1, "Milliseconds a session with an open run and no tool call keeps its device before closing the run and releasing it."),
			"deviceAcquireTimeoutMs": integerMin(1, "Milliseconds a device acquisition may take, excluding the runner build."),
		},
		nil,
		"Device session timeouts.",
	)
}

func suiteEntrySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"tags":  arraySchema(stringMinLen(1, ""), 0, "Select tests whose tags include any of these values."),
			"globs": arraySchema(stringMinLen(1, ""), 0, "Select tests whose file path matches any of these globs."),
		},
		nil,
		"One named test selector.",
	)
}

func atomyxConfigSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"project":       stringMinLen(1, "Human-readable project name."),
			"apps":          arraySchema(ref("AppEntry"), 1, "App variants this project can run."),
			"environments":  recordSchema(ref("EnvironmentEntry"), "Named environments, keyed by environment name."),
			"launch":        ref("LaunchPolicy"),
			"preconditions": stringEnum([]string{PreconditionsSkip, PreconditionsForce}, "Project-wide precondition override."),
			"report":        ref("Report"),
			"session":       ref("Session"),
			"suites":        recordSchema(ref("SuiteEntry"), "Named test selectors, keyed by suite name."),
			"testRoots":     arraySchema(stringMinLen(1, ""), 0, "Directories the CLI searches for test scripts."),
			"dataRoots":     arraySchema(stringMinLen(1, ""), 0, "Directories the CLI searches for data-driven fixtures."),
		},
		[]string{"project", "apps", "environments"},
		"Top-level Atomyx project config.",
	)
}
