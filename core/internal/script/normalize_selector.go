package script

// Selector expansion: turning the shorthand forms a YAML script can
// use for a selector into a canonical ScriptSelector or StringOrSelector.

// selectorFieldsFromMap validates a raw selector mapping's fields
// against ScriptSelector's field set, rejecting unknown keys and
// wrong-typed values, without the text-to-label mirroring
// expandSelectorShorthand applies. This is the shape zod's
// ScriptSelectorSchema itself checks.
func selectorFieldsFromMap(m map[string]any) (*ScriptSelector, error) {
	sel := &ScriptSelector{}
	for k, v := range m {
		switch k {
		case "text":
			s, ok := asString(v)
			if !ok {
				return nil, newParseError("selector.text: expected a string")
			}
			sel.Text = &s
		case "id":
			s, ok := asString(v)
			if !ok {
				return nil, newParseError("selector.id: expected a string")
			}
			sel.ID = &s
		case "label":
			s, ok := asString(v)
			if !ok {
				return nil, newParseError("selector.label: expected a string")
			}
			sel.Label = &s
		case "hint":
			s, ok := asString(v)
			if !ok {
				return nil, newParseError("selector.hint: expected a string")
			}
			sel.Hint = &s
		case "role":
			s, ok := asString(v)
			if !ok {
				return nil, newParseError("selector.role: expected a string")
			}
			sel.Role = &s
		case "nth":
			n, ok := asInt(v)
			if !ok || n < 0 {
				return nil, newParseError("selector.nth: expected a nonnegative integer")
			}
			sel.Nth = &n
		default:
			return nil, newParseError("selector: unknown field %q", k)
		}
	}
	return sel, nil
}

// expandSelectorShorthand expands a YAML selector value into a
// canonical ScriptSelector: a bare string becomes a visible-content
// match (text and label both set), and an object passes through
// field-checked, with label mirrored from text when text is set and
// label is not.
func expandSelectorShorthand(value any) (*ScriptSelector, error) {
	if s, ok := asString(value); ok {
		return &ScriptSelector{Text: &s, Label: &s}, nil
	}
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("Invalid selector: expected string or object, got %s", goType(value))
	}
	sel, err := selectorFieldsFromMap(m)
	if err != nil {
		return nil, err
	}
	if sel.Text != nil && sel.Label == nil {
		sel.Label = sel.Text
	}
	return sel, nil
}

// splitSelectorAndTimeout separates a waitFor / assert* YAML value
// into a selector and an optional timeoutMs, mirroring
// splitSelectorAndTimeout in step-normalizer.ts.
func splitSelectorAndTimeout(value any) (*ScriptSelector, *int, error) {
	if s, ok := asString(value); ok {
		sel, err := expandSelectorShorthand(s)
		return sel, nil, err
	}
	m, ok := asMap(value)
	if !ok {
		return nil, nil, newParseError("Invalid step value: expected string or object, got %s", goType(value))
	}
	var timeoutMs *int
	selectorFields := make(map[string]any, len(m))
	for k, v := range m {
		if k == "timeout" {
			if n, ok := asInt(v); ok {
				timeoutMs = &n
			}
			continue
		}
		selectorFields[k] = v
	}
	sel, err := expandSelectorShorthand(selectorFields)
	if err != nil {
		return nil, nil, err
	}
	return sel, timeoutMs, nil
}

// stringOrSelectorFromRaw validates the visible / notVisible union
// HandleCondition and PreconditionCheck accept: a bare string, or a
// selector object checked field-by-field but NOT expanded (no
// text-to-label mirroring. That mirroring is specific to
// expandSelectorShorthand and does not apply to this union).
func stringOrSelectorFromRaw(v any) (*StringOrSelector, error) {
	if s, ok := asString(v); ok {
		return &StringOrSelector{Str: &s}, nil
	}
	if m, ok := asMap(v); ok {
		sel, err := selectorFieldsFromMap(m)
		if err != nil {
			return nil, err
		}
		return &StringOrSelector{Selector: sel}, nil
	}
	return nil, newParseError("expected a string or a selector object, got %s", goType(v))
}
