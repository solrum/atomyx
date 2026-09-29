package script

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Pointer-gesture normalization: expanding a `pointer` step's touches,
// their actions, targets, and offsets from raw YAML.

func isJSNumber(v any) bool {
	switch v.(type) {
	case int, int64, float64:
		return true
	}
	return false
}

func normalizePointerMoveDuration(raw any) (*float64, error) {
	if raw == nil {
		return nil, nil
	}
	f, ok := asFloat(raw)
	if !ok {
		return nil, newParseError("pointer.moveDurationMs: expected number, got %s", goType(raw))
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, newParseError("pointer.moveDurationMs: expected a finite number, got %s", goType(raw))
	}
	return &f, nil
}

func normalizePointer(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("pointer: expected an object with a `touches` list")
	}
	touchesSlice, ok := asSlice(m["touches"])
	if !ok {
		return nil, newParseError("pointer: expected a `touches` list")
	}
	if len(touchesSlice) == 0 {
		return nil, newParseError("pointer: `touches` must have at least one entry")
	}

	moveDurationMs, err := normalizePointerMoveDuration(m["moveDurationMs"])
	if err != nil {
		return nil, err
	}

	touches, err := normalizePointerTouches(touchesSlice)
	if err != nil {
		return nil, err
	}
	return &PointerStep{Command: CommandPointer, Touches: touches, MoveDurationMs: moveDurationMs}, nil
}

func normalizePointerTouches(raw []any) ([]PointerTouch, error) {
	touches := make([]PointerTouch, 0, len(raw))
	for i, entry := range raw {
		m, ok := asMap(entry)
		if !ok {
			return nil, newParseError("pointer.touches[%d]: expected an object with `actions` and an optional `id`", i)
		}
		id := ""
		if idRaw, present := m["id"]; present && idRaw != nil {
			s, ok := asString(idRaw)
			if !ok {
				return nil, newParseError("pointer.touches[%d].id: expected a string, got %s", i, goType(idRaw))
			}
			id = s
		}
		actionsRaw, ok := asSlice(m["actions"])
		if !ok {
			return nil, newParseError("pointer.touches[%d]: missing `actions` array", i)
		}
		actions, err := normalizePointerActions(actionsRaw, i)
		if err != nil {
			return nil, err
		}
		touches = append(touches, PointerTouch{ID: id, Actions: actions})
	}
	return touches, nil
}

func normalizePointerActions(raw []any, touchIndex int) ([]PointerAction, error) {
	actions := make([]PointerAction, 0, len(raw))
	for i, entry := range raw {
		path := fmt.Sprintf("pointer.touches[%d].actions[%d]", touchIndex, i)
		a, err := normalizePointerAction(entry, path)
		if err != nil {
			return nil, err
		}
		actions = append(actions, *a)
	}
	return actions, nil
}

func normalizePointerAction(raw any, path string) (*PointerAction, error) {
	if s, ok := asString(raw); ok {
		switch s {
		case "up":
			return &PointerAction{Type: "up"}, nil
		case "capture":
			return &PointerAction{Type: "capture"}, nil
		}
		return nil, newParseError("%s: bare string must be \"up\" or \"capture\", got %q", path, s)
	}
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("%s: expected string or object, got %s", path, goType(raw))
	}
	if len(m) != 1 {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, newParseError("%s: expected a single-key object, got keys [%s]", path, strings.Join(keys, ", "))
	}
	var key string
	var value any
	for k, v := range m {
		key, value = k, v
	}

	switch key {
	case "down":
		target, err := normalizePointerTarget(value, path, "down")
		if err != nil {
			return nil, err
		}
		pressure, err := extractPressure(value, path, "down")
		if err != nil {
			return nil, err
		}
		return &PointerAction{Type: "down", Target: target, Pressure: pressure}, nil
	case "move":
		target, err := normalizePointerTarget(value, path, "move")
		if err != nil {
			return nil, err
		}
		pressure, err := extractPressure(value, path, "move")
		if err != nil {
			return nil, err
		}
		return &PointerAction{Type: "move", Target: target, Pressure: pressure}, nil
	case "wait":
		f, ok := asFloat(value)
		if !ok {
			return nil, newParseError("%s: wait expects a number (ms), got %s", path, goType(value))
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, newParseError("%s: wait expects a finite number (ms), got %s", path, goType(value))
		}
		return &PointerAction{Type: "wait", Ms: &f}, nil
	case "up":
		return &PointerAction{Type: "up"}, nil
	case "capture":
		if value == nil {
			return &PointerAction{Type: "capture"}, nil
		}
		label, ok := asString(value)
		if !ok {
			return nil, newParseError("%s: capture expects a string label or no value, got %s", path, goType(value))
		}
		if label == "" {
			return nil, newParseError("%s: capture label must not be empty", path)
		}
		return &PointerAction{Type: "capture", Label: &label}, nil
	default:
		return nil, newParseError("%s: unknown action %q (expected down / move / wait / up / capture)", path, key)
	}
}

func extractPressure(raw any, path string, action string) (*float64, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, nil
	}
	v, present := m["pressure"]
	if !present || v == nil {
		return nil, nil
	}
	f, ok := asFiniteFloat(v)
	if !ok || f < 0 || f > 1 {
		return nil, newParseError("%s.%s: pressure must be a number in [0.0, 1.0]", path, action)
	}
	return &f, nil
}

func normalizePointerTarget(raw any, path string, action string) (*PointerTarget, error) {
	if s, ok := asString(raw); ok {
		sel, err := expandSelectorShorthand(s)
		if err != nil {
			return nil, err
		}
		return &PointerTarget{Selector: sel}, nil
	}
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("%s.%s: expected selector or { x, y }, got %s", path, action, goType(raw))
	}

	if isJSNumber(m["x"]) && isJSNumber(m["y"]) {
		xf, _ := asFloat(m["x"])
		yf, _ := asFloat(m["y"])
		if math.IsInf(xf, 0) || math.IsNaN(xf) || math.IsInf(yf, 0) || math.IsNaN(yf) {
			return nil, newParseError("%s.%s: x and y must be finite numbers", path, action)
		}
		return &PointerTarget{Point: &Point{X: xf, Y: yf}}, nil
	}

	if _, hasSelector := m["selector"]; hasSelector {
		return normalizePointerSelectorTarget(m, path, action)
	}
	if _, hasAnchor := m["anchor"]; hasAnchor {
		return nil, newParseError("%s.%s: `anchor`/`offset` require an explicit `selector:` key", path, action)
	}
	if _, hasOffset := m["offset"]; hasOffset {
		return nil, newParseError("%s.%s: `anchor`/`offset` require an explicit `selector:` key", path, action)
	}

	sel, err := expandSelectorShorthand(m)
	if err != nil {
		return nil, err
	}
	return &PointerTarget{Selector: sel}, nil
}

func normalizePointerSelectorTarget(obj map[string]any, path string, action string) (*PointerTarget, error) {
	sel, err := expandSelectorShorthand(obj["selector"])
	if err != nil {
		return nil, err
	}
	target := &PointerTarget{Selector: sel}
	if anchorRaw, present := obj["anchor"]; present && anchorRaw != nil {
		s, ok := asString(anchorRaw)
		if !ok {
			return nil, newParseError("%s.%s.anchor: expected a string, got %s", path, action, goType(anchorRaw))
		}
		if !isValidPointerAnchor(s) {
			return nil, newParseError("%s.%s.anchor: unknown anchor %q", path, action, s)
		}
		target.Anchor = &s
	}
	if offsetRaw, present := obj["offset"]; present && offsetRaw != nil {
		off, err := normalizePointerOffset(offsetRaw, path, action)
		if err != nil {
			return nil, err
		}
		target.Offset = off
	}
	return target, nil
}

func normalizePointerOffset(raw any, path string, action string) (*PointerOffset, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("%s.%s.offset: expected an object with dx/dy, got %s", path, action, goType(raw))
	}
	for k := range m {
		if k != "dx" && k != "dy" {
			return nil, newParseError("%s.%s.offset.%s: unknown offset field", path, action, k)
		}
	}
	off := &PointerOffset{}
	if v, present := m["dx"]; present && v != nil {
		f, ok := asFiniteFloat(v)
		if !ok {
			return nil, newParseError("%s.%s.offset.dx: expected a finite number", path, action)
		}
		off.Dx = &f
	}
	if v, present := m["dy"]; present && v != nil {
		f, ok := asFiniteFloat(v)
		if !ok {
			return nil, newParseError("%s.%s.offset.dy: expected a finite number", path, action)
		}
		off.Dy = &f
	}
	return off, nil
}
