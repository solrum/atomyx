package script

import "strings"

// Normalizers for the steps that control an app's process and data.

// normalizeLaunchApp accepts a bare `launchApp` or an object with the
// optional fields `clearState`, `stopApp` and `arguments`.
func normalizeLaunchApp(value any) (ScriptStep, error) {
	step := &LaunchAppStep{Command: CommandLaunchApp}
	if value == nil {
		return step, nil
	}
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("launchApp: expected no value or an object with `clearState`, `stopApp` and `arguments`, got %s", goType(value))
	}
	clearState, err := normalizeBoolField(m["clearState"], "launchApp.clearState")
	if err != nil {
		return nil, err
	}
	stopApp, err := normalizeBoolField(m["stopApp"], "launchApp.stopApp")
	if err != nil {
		return nil, err
	}
	arguments, err := normalizeLaunchArguments(m["arguments"])
	if err != nil {
		return nil, err
	}
	step.ClearState = clearState
	step.StopApp = stopApp
	step.Arguments = arguments
	return step, nil
}

// normalizeLaunchArguments reads the optional `arguments` mapping of a
// launchApp step. Each value must be a string, number or boolean and is
// stored as the text a script author would read it as. It returns nil
// when the field is absent or empty.
func normalizeLaunchArguments(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("launchApp.arguments: must be a mapping of names to values, got %s", goType(raw))
	}
	if len(m) == 0 {
		return nil, nil
	}
	arguments := make(map[string]string, len(m))
	for key, value := range m {
		switch value.(type) {
		case string, bool, int, int64, float64:
			arguments[key] = jsString(value)
		default:
			return nil, newParseError("launchApp.arguments.%s: must be a string, number or boolean, got %s", key, goType(value))
		}
	}
	return arguments, nil
}

func normalizeStopApp(value any) (ScriptStep, error) {
	appID, err := normalizeAppIDArgument(value, CommandStopApp)
	if err != nil {
		return nil, err
	}
	return &StopAppStep{Command: CommandStopApp, AppID: appID}, nil
}

func normalizeKillApp(value any) (ScriptStep, error) {
	appID, err := normalizeAppIDArgument(value, CommandKillApp)
	if err != nil {
		return nil, err
	}
	return &KillAppStep{Command: CommandKillApp, AppID: appID}, nil
}

func normalizeClearState(value any) (ScriptStep, error) {
	appID, err := normalizeAppIDArgument(value, CommandClearState)
	if err != nil {
		return nil, err
	}
	return &ClearStateStep{Command: CommandClearState, AppID: appID}, nil
}

// normalizeAppIDArgument reads the optional app id of an app lifecycle
// step, written as a bare value (`stopApp: com.example.app`) or as an
// object (`stopApp: {appId: com.example.app}`). It returns nil when the
// step names no app, which means the script's own appId.
func normalizeAppIDArgument(value any, command string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if m, ok := asMap(value); ok {
		raw, present := m["appId"]
		if !present || raw == nil {
			return nil, nil
		}
		value = raw
	}
	id, ok := asString(value)
	if !ok {
		return nil, newParseError("%s: appId must be a string, got %s", command, goType(value))
	}
	return &id, nil
}

// normalizeBoolField reads an optional boolean field. The strings
// "true" and "false" are accepted because a `${var}` placeholder
// expands to text. It returns nil when the field is absent.
func normalizeBoolField(raw any, path string) (*bool, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case bool:
		return &v, nil
	case string:
		switch strings.TrimSpace(v) {
		case "true":
			parsed := true
			return &parsed, nil
		case "false":
			parsed := false
			return &parsed, nil
		}
	}
	return nil, newParseError("%s: must be true or false, got %s", path, jsString(raw))
}
