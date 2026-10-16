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
			if err := patternField("text", v, &sel.Text, &sel.TextRegex); err != nil {
				return nil, err
			}
		case "id":
			if err := patternField("id", v, &sel.ID, &sel.IDRegex); err != nil {
				return nil, err
			}
		case "label":
			if err := patternField("label", v, &sel.Label, &sel.LabelRegex); err != nil {
				return nil, err
			}
		case "hint":
			if err := patternField("hint", v, &sel.Hint, &sel.HintRegex); err != nil {
				return nil, err
			}
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

// patternField decodes one content field of a selector mapping: a
// string sets literal, and a {regex, flags} object sets pattern after
// compiling it, so an invalid expression or an unknown flag fails at
// parse time.
func patternField(name string, v any, literal **string, pattern **RegexPattern) error {
	if s, ok := asString(v); ok {
		*literal = &s
		return nil
	}
	m, ok := asMap(v)
	if !ok {
		return newParseError("selector.%s: expected a string or a {regex, flags} object", name)
	}
	p := RegexPattern{}
	for k, raw := range m {
		s, isString := asString(raw)
		switch {
		case k == "regex" && isString:
			p.Regex = s
		case k == "flags" && isString:
			p.Flags = s
		case k == "regex" || k == "flags":
			return newParseError("selector.%s.%s: expected a string", name, k)
		default:
			return newParseError("selector.%s: unknown field %q, expected regex and flags", name, k)
		}
	}
	if _, present := m["regex"]; !present {
		return newParseError("selector.%s: a pattern object requires a regex field", name)
	}
	if _, err := p.Compile(); err != nil {
		return newParseError("selector.%s: invalid regex %q: %s", name, p.Regex, err.Error())
	}
	*pattern = &p
	return nil
}

// expandSelectorShorthand expands a YAML selector value into a
// canonical ScriptSelector: a bare string becomes a visible-content
// match (text and label both set), and an object passes through
// field-checked, with label mirrored from text when text is set and
// label is not. A regex text is mirrored to label the same way.
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
	if sel.Text != nil && sel.Label == nil && sel.LabelRegex == nil {
		sel.Label = sel.Text
	}
	if sel.TextRegex != nil && sel.Label == nil && sel.LabelRegex == nil {
		sel.LabelRegex = sel.TextRegex
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
