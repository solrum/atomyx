package script

// Normalizers for the keyboard, scroll and wait steps.

func normalizeHideKeyboard(any) (ScriptStep, error) {
	return &HideKeyboardStep{Command: CommandHideKeyboard}, nil
}

// normalizeEraseText accepts a bare `eraseText`, a count
// (`eraseText: 5`) or an object (`eraseText: {charactersToErase: 5}`).
func normalizeEraseText(value any) (ScriptStep, error) {
	raw := value
	if m, ok := asMap(value); ok {
		raw = m["charactersToErase"]
	}
	count, err := normalizeIntField(raw, "eraseText.charactersToErase")
	if err != nil {
		return nil, err
	}
	return &EraseTextStep{Command: CommandEraseText, Characters: count}, nil
}

// normalizeWaitForAnimationToEnd accepts a bare `waitForAnimationToEnd`,
// a timeout in milliseconds (`waitForAnimationToEnd: 3000`) or an object
// (`waitForAnimationToEnd: {timeout: 3000}`).
func normalizeWaitForAnimationToEnd(value any) (ScriptStep, error) {
	raw := value
	if m, ok := asMap(value); ok {
		raw = m["timeout"]
	}
	timeoutMs, err := normalizeIntField(raw, "waitForAnimationToEnd.timeout")
	if err != nil {
		return nil, err
	}
	return &WaitForAnimationToEndStep{Command: CommandWaitForAnimationToEnd, TimeoutMs: timeoutMs}, nil
}

// normalizeScrollUntilVisible accepts a selector in any form a tap
// does, plus an optional `direction` beside the selector fields of an
// object.
func normalizeScrollUntilVisible(value any) (ScriptStep, error) {
	step := &ScrollUntilVisibleStep{Command: CommandScrollUntilVisible}
	if m, ok := asMap(value); ok {
		fields := make(map[string]any, len(m))
		for k, v := range m {
			if k == "direction" {
				if v == nil {
					continue
				}
				direction, ok := asString(v)
				if !ok {
					return nil, newParseError("scrollUntilVisible.direction: expected a string, got %s", goType(v))
				}
				step.Direction = &direction
				continue
			}
			fields[k] = v
		}
		value = fields
	}
	sel, err := expandSelectorShorthand(value)
	if err != nil {
		return nil, err
	}
	step.Selector = *sel
	return step, nil
}

// normalizeInputRandom accepts the kind as a bare string
// (`inputRandom: email`) or an object (`inputRandom: {kind: text,
// length: 12}`). An unknown or missing kind is left for validation to
// report with its path.
func normalizeInputRandom(value any) (ScriptStep, error) {
	step := &InputRandomStep{Command: CommandInputRandom}
	if kind, ok := asString(value); ok {
		step.Kind = kind
		return step, nil
	}
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("inputRandom: expected a kind such as `text`, or an object with `kind` and `length`, got %s", goType(value))
	}
	if raw := m["kind"]; raw != nil {
		kind, ok := asString(raw)
		if !ok {
			return nil, newParseError("inputRandom.kind: expected a string, got %s", goType(raw))
		}
		step.Kind = kind
	}
	length, err := normalizeIntField(m["length"], "inputRandom.length")
	if err != nil {
		return nil, err
	}
	step.Length = length
	return step, nil
}
