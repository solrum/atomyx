package script

// High-level gesture-sugar normalization: expanding a `longPress`,
// `pinch`, or `rotate` step's fields from raw YAML. Each step's
// location fields (`at`, `center`, `thenSwipeTo`) reuse the same
// target union a raw pointer action's target does, so they accept an
// absolute coordinate or a selector interchangeably.

func normalizeGestureLocation(m map[string]any, key string, command string) (*PointerTarget, error) {
	raw, present := m[key]
	if !present || raw == nil {
		return nil, newParseError("%s.%s: required, expected a selector or { x, y }", command, key)
	}
	return normalizePointerTarget(raw, command, key)
}

func normalizeGestureNumber(m map[string]any, key string, command string) (*float64, error) {
	raw, present := m[key]
	if !present || raw == nil {
		return nil, nil
	}
	f, ok := asFiniteFloat(raw)
	if !ok {
		return nil, newParseError("%s.%s: expected a finite number, got %s", command, key, goType(raw))
	}
	return &f, nil
}

func normalizeLongPress(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("longPress: expected an object with `at`")
	}
	at, err := normalizeGestureLocation(m, "at", CommandLongPress)
	if err != nil {
		return nil, err
	}
	holdMs, err := normalizeGestureNumber(m, "holdMs", CommandLongPress)
	if err != nil {
		return nil, err
	}
	step := &LongPressStep{Command: CommandLongPress, At: at, HoldMs: holdMs}
	if raw, present := m["thenSwipeTo"]; present && raw != nil {
		target, err := normalizePointerTarget(raw, CommandLongPress, "thenSwipeTo")
		if err != nil {
			return nil, err
		}
		step.ThenSwipeTo = target
	}
	return step, nil
}

func normalizePinch(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("pinch: expected an object with `center`, `startGap`, and `endGap`")
	}
	center, err := normalizeGestureLocation(m, "center", CommandPinch)
	if err != nil {
		return nil, err
	}
	startGap, ok := asFiniteFloat(m["startGap"])
	if !ok {
		return nil, newParseError("pinch.startGap: expected a finite number, got %s", goType(m["startGap"]))
	}
	endGap, ok := asFiniteFloat(m["endGap"])
	if !ok {
		return nil, newParseError("pinch.endGap: expected a finite number, got %s", goType(m["endGap"]))
	}
	durationMs, err := normalizeGestureNumber(m, "durationMs", CommandPinch)
	if err != nil {
		return nil, err
	}
	step := &PinchStep{Command: CommandPinch, Center: center, StartGap: startGap, EndGap: endGap, DurationMs: durationMs}
	if raw, present := m["axis"]; present && raw != nil {
		s, ok := asString(raw)
		if !ok {
			return nil, newParseError("pinch.axis: expected a string, got %s", goType(raw))
		}
		step.Axis = &s
	}
	return step, nil
}

func normalizeRotate(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("rotate: expected an object with `center` and `degrees`")
	}
	center, err := normalizeGestureLocation(m, "center", CommandRotate)
	if err != nil {
		return nil, err
	}
	degrees, ok := asFiniteFloat(m["degrees"])
	if !ok {
		return nil, newParseError("rotate.degrees: expected a finite number, got %s", goType(m["degrees"]))
	}
	radius, err := normalizeGestureNumber(m, "radius", CommandRotate)
	if err != nil {
		return nil, err
	}
	durationMs, err := normalizeGestureNumber(m, "durationMs", CommandRotate)
	if err != nil {
		return nil, err
	}
	return &RotateStep{Command: CommandRotate, Center: center, Degrees: degrees, Radius: radius, DurationMs: durationMs}, nil
}
