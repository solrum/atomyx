package runfolder

// GenerateJSONSchema returns the JSON Schema of meta.json. The run id
// has no pattern, because a migrated run keeps its legacy id.
func GenerateJSONSchema() map[string]any {
	return map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$id":     "https://schemas.atomyx.dev/run.schema.json",
		"$ref":    "#/definitions/RunMeta",
		"definitions": map[string]any{
			"Device":  deviceSchema(),
			"Finding": findingSchema(),
			"Host":    hostSchema(),
			"MCPInfo": mcpInfoSchema(),
			"RunMeta": metaSchema(),
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
		"schemaVersion": map[string]any{"type": "integer", "minimum": 1},
		"runId":         map[string]any{"type": "string", "minLength": 1},
		"kind":          enum(Kinds(), "What started the run."),
		"scriptPath":    str(),
		"scriptName":    str(),
		"deviceId":      str(),
		"device":        ref("Device"),
		"host":          ref("Host"),
		"startedAt":     epochMs("Epoch milliseconds when the run started."),
		"completedAt":   epochMs("Epoch milliseconds when the run finished. Absent for a run that never finished."),
		"result":        enum(Results(), "Coarse verdict."),
		"outcome":       enum(Outcomes(), "Precise reason the run ended."),
		"failedAtStep":  map[string]any{"type": "integer", "minimum": 0},
		"totalSteps":    map[string]any{"type": "integer", "minimum": 0},
		"mcp":           ref("MCPInfo"),
		"importedFrom":  map[string]any{"type": "string", "description": "Id of the record of another store this folder was imported from. Absent for a run written natively."},
	}, []string{"schemaVersion", "runId", "kind", "startedAt"})
}
