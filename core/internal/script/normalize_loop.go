package script

import (
	"strconv"
	"strings"
)

// Normalizers for the block steps that run a nested command list
// repeatedly.

// normalizeCommandList normalizes the inline `commands` list of a block
// step. An absent list normalizes to an empty one, which validation
// rejects with a path-bearing message.
func normalizeCommandList(raw any, owner string) ([]ScriptStep, error) {
	if raw == nil {
		return []ScriptStep{}, nil
	}
	arr, ok := asSlice(raw)
	if !ok {
		return nil, newParseError("%s.commands: expected a step list", owner)
	}
	steps := make([]ScriptStep, 0, len(arr))
	for i, item := range arr {
		step, err := normalizeStep(item, i)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// normalizeIntField reads an optional whole-number field. A numeric
// string is accepted because a `${var}` placeholder expands to text.
// It returns nil when the field is absent.
func normalizeIntField(raw any, path string) (*int, error) {
	if raw == nil {
		return nil, nil
	}
	if n, ok := asInt(raw); ok {
		return &n, nil
	}
	if s, ok := asString(raw); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return &n, nil
		}
	}
	return nil, newParseError("%s: must be a whole number, got %s", path, jsString(raw))
}

func normalizeRepeat(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("repeat: expected an object with `commands` and `times` or `while`")
	}
	step := &RepeatStep{Command: CommandRepeat}
	times, err := normalizeIntField(m["times"], "repeat.times")
	if err != nil {
		return nil, err
	}
	step.Times = times
	if raw, present := m["while"]; present && raw != nil {
		cond, err := normalizeRepeatCondition(raw)
		if err != nil {
			return nil, err
		}
		step.While = cond
	}
	commands, err := normalizeCommandList(m["commands"], "repeat")
	if err != nil {
		return nil, err
	}
	step.Commands = commands
	return step, nil
}

func normalizeRepeatCondition(raw any) (*RepeatCondition, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("repeat.while: expected an object with one of `visible`, `notVisible` or `expr`")
	}
	cond := &RepeatCondition{}
	if v, present := m["visible"]; present && v != nil {
		sv, err := stringOrSelectorFromRaw(v)
		if err != nil {
			return nil, err
		}
		cond.Visible = sv
	}
	if v, present := m["notVisible"]; present && v != nil {
		sv, err := stringOrSelectorFromRaw(v)
		if err != nil {
			return nil, err
		}
		cond.NotVisible = sv
	}
	if v, present := m["expr"]; present && v != nil {
		expr := jsString(v)
		cond.Expr = &expr
	}
	return cond, nil
}
