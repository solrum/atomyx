package runfolder

// GenerateJSONSchema returns the JSON Schema of meta.json. The run id
// has no pattern, because a migrated run keeps its legacy id.
func GenerateJSONSchema() map[string]any {
	return map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$id":     "https://schemas.atomyx.dev/run.schema.json",
		"$ref":    "#/definitions/RunMeta",
		"definitions": map[string]any{
			"Device":                 deviceSchema(),
			"Finding":                findingSchema(),
			"Recording":              recordingSchema(),
			"Host":                   hostSchema(),
			"Intervention":           interventionSchema(),
			"InterventionEntry":      interventionEntrySchema(),
			"InterventionsTruncated": interventionsTruncatedSchema(),
			"MCPInfo":                mcpInfoSchema(),
			"RunMeta":                metaSchema(),
		},
	}
}

func str() map[string]any { return map[string]any{"type": "string"} }

func epochMs(description string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 0, "description": description}
}

func enum[T ~string](values []T, description string) map[string]any {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = string(v)
	}
	return map[string]any{"type": "string", "enum": names, "description": description}
}

func object(properties map[string]any, required []string) map[string]any {
	m := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": true,
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/definitions/" + name}
}

func deviceSchema() map[string]any {
	return object(map[string]any{
		"platform":   str(),
		"kind":       str(),
		"hardwareId": str(),
		"name":       str(),
	}, []string{"platform"})
}

func hostSchema() map[string]any {
	return object(map[string]any{
		"atomyxVersion": str(),
		"agentVersion":  str(),
	}, nil)
}

func findingSchema() map[string]any {
	return object(map[string]any{
		"id":             str(),
		"title":          str(),
		"description":    str(),
		"screenshotPath": str(),
		"timestamp":      epochMs("Epoch milliseconds."),
	}, []string{"id", "title", "description", "timestamp"})
}

func recordingSchema() map[string]any {
	return object(map[string]any{
		"state": map[string]any{"type": "string", "enum": []string{string(RecordingKept), string(RecordingUnsupported), string(RecordingError)}, "description": "How the screen recording ended."},
		"files": map[string]any{"type": "array", "items": str(), "description": "Video file names relative to the artifacts directory, oldest first. Present when state is kept."},
		"code":  map[string]any{"type": "string", "description": "Stable error code. Present when state is unsupported or error."},
	}, []string{"state"})
}

func mcpInfoSchema() map[string]any {
	return object(map[string]any{
		"name":        str(),
		"source":      str(),
		"actionCount": map[string]any{"type": "integer", "minimum": 0},
		"findings":    map[string]any{"type": "array", "items": ref("Finding")},
		"summary":     str(),
		"durationMs":  map[string]any{"type": "integer", "minimum": 0},
	}, []string{"name", "source", "actionCount", "durationMs"})
}

// Readers ignore unknown fields, so every object stays open
// (additionalProperties true).
func metaSchema() map[string]any {
	return object(map[string]any{
		"schemaVersion":        map[string]any{"type": "integer", "minimum": 1},
		"runId":                map[string]any{"type": "string", "minLength": 1},
		"kind":                 enum(Kinds(), "What started the run."),
		"scriptPath":           str(),
		"scriptName":           str(),
		"deviceId":             str(),
		"device":               ref("Device"),
		"host":                 ref("Host"),
		"startedAt":            epochMs("Epoch milliseconds when the run started."),
		"completedAt":          epochMs("Epoch milliseconds when the run finished. Absent for a run that never finished."),
		"result":               enum(Results(), "Coarse verdict."),
		"outcome":              enum(Outcomes(), "Precise reason the run ended."),
		"failedAtStep":         map[string]any{"type": "integer", "minimum": 0},
		"totalSteps":           map[string]any{"type": "integer", "minimum": 0},
		"mcp":                  ref("MCPInfo"),
		"importedFrom":         map[string]any{"type": "string", "description": "Id of the record of another store this folder was imported from. Absent for a run written natively."},
		"interventionsDropped": map[string]any{"type": "integer", "minimum": 1, "description": "How many intervention lines did not fit in interventions.jsonl. Absent when none were dropped."},
		"recording":            ref("Recording"),
	}, []string{"schemaVersion", "runId", "kind", "startedAt"})
}

// interventionEntrySchema describes one line of interventions.jsonl: an
// intervention, or the single marker that ends a file that reached its
// size bound.
func interventionEntrySchema() map[string]any {
	return map[string]any{"oneOf": []any{ref("Intervention"), ref("InterventionsTruncated")}}
}

func interventionSchema() map[string]any {
	return object(map[string]any{
		"kind":       map[string]any{"type": "string", "description": "What happened, such as viewerAttached, viewerDetached, controlBorrowed or humanAction."},
		"atMs":       epochMs("Epoch milliseconds when it happened."),
		"client":     map[string]any{"type": "string", "description": "Name of the process that acted."},
		"step":       map[string]any{"type": "string", "description": "A humanAction only: the step as the person wrote it. The text of a type step is always masked."},
		"command":    map[string]any{"type": "string", "description": "A humanAction only: the command name of the step."},
		"outcome":    map[string]any{"type": "string", "description": "A humanAction only: passed or failed."},
		"message":    map[string]any{"type": "string", "description": "A humanAction only: why the step failed, with secret values masked."},
		"durationMs": map[string]any{"type": "integer", "minimum": 0, "description": "A humanAction only: how long the step took."},
		"artifacts":  map[string]any{"type": "array", "items": str(), "description": "A humanAction only: run-relative files the step produced."},
	}, []string{"kind", "atMs", "client"})
}

func interventionsTruncatedSchema() map[string]any {
	return object(map[string]any{
		"truncated":  map[string]any{"type": "boolean", "const": true},
		"limitBytes": map[string]any{"type": "integer", "minimum": 1},
	}, []string{"truncated", "limitBytes"})
}
