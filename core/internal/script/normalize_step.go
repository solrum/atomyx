package script

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Per-command step normalizers: one function per script command,
// turning that command's raw YAML value into its canonical ScriptStep.

// captureShorthandRegex matches the "METHOD /path as: varName" inline
// form of a capture step's string shorthand.
var captureShorthandRegex = regexp.MustCompile(`^(.+?)\s+as:\s*(\w+)$`)

func normalizeLaunchApp(any) (ScriptStep, error) {
	return &LaunchAppStep{Command: CommandLaunchApp}, nil
}

func normalizeTap(value any) (ScriptStep, error) {
	sel, err := expandSelectorShorthand(value)
	if err != nil {
		return nil, err
	}
	return &TapStep{Command: CommandTap, Selector: *sel}, nil
}

func normalizeType(value any) (ScriptStep, error) {
	if s, ok := asString(value); ok {
		return &TypeStep{Command: CommandType, Text: s}, nil
	}
	m, _ := asMap(value)
	step := &TypeStep{Command: CommandType, Text: stringOrDefault(m["text"], "")}
	if into, present := m["into"]; present && into != nil {
		sel, err := expandSelectorShorthand(into)
		if err != nil {
			return nil, err
		}
		step.Into = sel
	}
	return step, nil
}

func normalizeWaitFor(value any) (ScriptStep, error) {
	sel, timeoutMs, err := splitSelectorAndTimeout(value)
	if err != nil {
		return nil, err
	}
	return &WaitForStep{Command: CommandWaitFor, Selector: *sel, TimeoutMs: timeoutMs}, nil
}

func normalizeAssertVisible(value any) (ScriptStep, error) {
	sel, timeoutMs, err := splitSelectorAndTimeout(value)
	if err != nil {
		return nil, err
	}
	return &AssertVisibleStep{Command: CommandAssertVisible, Selector: *sel, TimeoutMs: timeoutMs}, nil
}

func normalizeAssertNotVisible(value any) (ScriptStep, error) {
	sel, timeoutMs, err := splitSelectorAndTimeout(value)
	if err != nil {
		return nil, err
	}
	return &AssertNotVisibleStep{Command: CommandAssertNotVisible, Selector: *sel, TimeoutMs: timeoutMs}, nil
}

func normalizeAssertExists(value any) (ScriptStep, error) {
	sel, timeoutMs, err := splitSelectorAndTimeout(value)
	if err != nil {
		return nil, err
	}
	return &AssertExistsStep{Command: CommandAssertExists, Selector: *sel, TimeoutMs: timeoutMs}, nil
}

func normalizeScreenshot(value any) (ScriptStep, error) {
	step := &ScreenshotStep{Command: CommandScreenshot}
	if s, ok := asString(value); ok {
		step.Label = &s
	}
	return step, nil
}

func normalizeSwipe(value any) (ScriptStep, error) {
	return &SwipeStep{Command: CommandSwipe, Direction: jsString(value)}, nil
}

func normalizePressKey(value any) (ScriptStep, error) {
	return &PressKeyStep{Command: CommandPressKey, Key: jsString(value)}, nil
}

func normalizeBack(any) (ScriptStep, error) {
	return &BackStep{Command: CommandBack}, nil
}

// normalizeSleep accepts a sleep duration given as a number or a
// numeric string, but ms must be a nonnegative integer: a fractional
// value has no well-defined truncation the caller asked for, so it is
// rejected rather than silently rounded.
func normalizeSleep(value any) (ScriptStep, error) {
	if n, ok := asInt(value); ok {
		if n < 0 {
			return nil, newParseError("sleep: ms must be a nonnegative integer, got %d", n)
		}
		return &SleepStep{Command: CommandSleep, Ms: n}, nil
	}
	if isJSNumber(value) {
		return nil, newParseError("sleep: ms must be a nonnegative integer, got %s", jsString(value))
	}
	n, err := strconv.Atoi(strings.TrimSpace(jsString(value)))
	if err != nil || n < 0 {
		return nil, newParseError("sleep: ms must be a nonnegative integer, got %q", jsString(value))
	}
	return &SleepStep{Command: CommandSleep, Ms: n}, nil
}

func normalizeCapture(value any) (ScriptStep, error) {
	if s, ok := asString(value); ok {
		match := captureShorthandRegex.FindStringSubmatch(s)
		if match == nil {
			return nil, newParseError(`Invalid capture format: %q. Expected: "METHOD /path as: varName"`, s)
		}
		return &CaptureStep{Command: CommandCapture, Pattern: strings.TrimSpace(match[1]), As: match[2]}, nil
	}
	m, _ := asMap(value)
	pattern := ""
	if v, ok := m["url"]; ok && v != nil {
		pattern = jsString(v)
	} else if v, ok := m["pattern"]; ok && v != nil {
		pattern = jsString(v)
	}
	return &CaptureStep{Command: CommandCapture, Pattern: pattern, As: stringOrDefault(m["as"], "")}, nil
}

func normalizeAssertApi(value any) (ScriptStep, error) {
	m, _ := asMap(value)
	step := &AssertApiStep{Command: CommandAssertApi, From: stringOrDefault(m["from"], "")}
	if v, ok := m["status"]; ok {
		if n, ok := asInt(v); ok {
			step.Status = &n
		}
	}
	if v, ok := m["body"]; ok {
		if om, ok := asOrderedMap(v); ok {
			step.Body = &om
		}
	}
	return step, nil
}

func normalizeExtract(value any) (ScriptStep, error) {
	m, _ := asMap(value)
	step := &ExtractStep{Command: CommandExtract, From: stringOrDefault(m["from"], ""), Values: &OrderedStringMap{}}
	if v, ok := m["values"]; ok && v != nil {
		om, ok := asOrderedMap(v)
		if !ok {
			return nil, newParseError("extract.values: expected an object")
		}
		var issues []extractValueTypeIssue
		for _, k := range om.Keys() {
			vv, _ := om.Get(k)
			s, ok := asString(vv)
			if !ok {
				issues = append(issues, extractValueTypeIssue{key: k, received: zodReceivedType(vv)})
				continue
			}
			step.Values.Set(k, s)
		}
		if len(issues) > 0 {
			return nil, &extractValuesError{issues: issues}
		}
	}
	return step, nil
}

// extractValueTypeIssue is one non-string entry found in an extract
// step's `values` map: the key, and the entry's dynamic type rendered
// with zod's own type-name vocabulary (see zodReceivedType).
type extractValueTypeIssue struct {
	key      string
	received string
}

// extractValuesError carries every non-string `values` entry found by
// normalizeExtract, deferred so normalizeStep, the caller that knows
// the step's index, can render the zod-style issue message
// for a string-record failure: one
// "Expected string, received <type>" line per entry, addressed by its
// full "steps.<n>.values.<key>" path.
type extractValuesError struct {
	issues []extractValueTypeIssue
}

func (e *extractValuesError) Error() string {
	return "extract.values: one or more values are not strings"
}

// renderExtractValuesError turns ev, discovered while normalizing the
// step at stepIndex, into the "Invalid script:" message for
// the extract.values input:
// one "  at steps.<n>.values.<key>: Expected string, received <type>"
// line per non-string entry, in declaration order.
func renderExtractValuesError(stepIndex int, ev *extractValuesError) *ParseError {
	lines := make([]string, len(ev.issues))
	for i, issue := range ev.issues {
		lines[i] = fmt.Sprintf("  at steps.%d.values.%s: Expected string, received %s", stepIndex, issue.key, issue.received)
	}
	return newParseError("Invalid script:\n%s", strings.Join(lines, "\n"))
}

// zodReceivedType renders v's dynamic shape using zod's own
// `getParsedType` vocabulary (object/array/string/boolean/number/
// null), for a decoded Go value. The token appears
// verbatim in a script error message that a golden pins, so it must
// keep zod's naming even where goType (used for this package's other
// messages) picks a different word.
func zodReceivedType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case map[string]any, OrderedMap:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int64, float64:
		return "number"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func normalizeRead(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("read: expected an object with `selector` and `as`")
	}
	sel, err := expandSelectorShorthand(m["selector"])
	if err != nil {
		return nil, err
	}
	step := &ReadStep{Command: CommandRead, Selector: *sel, As: stringOrDefault(m["as"], "")}
	if v, present := m["field"]; present && v != nil {
		s := jsString(v)
		step.Field = &s
	}
	return step, nil
}

func normalizeAssertExpr(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("assertExpr: expected an object with `expr`")
	}
	return &AssertExprStep{Command: CommandAssertExpr, Expr: stringOrDefault(m["expr"], "")}, nil
}

func normalizeDoValue(raw any) (*StepsOrFile, error) {
	if s, ok := asString(raw); ok {
		return &StepsOrFile{File: &s}, nil
	}
	arr, ok := asSlice(raw)
	if !ok {
		return nil, newParseError("expected a step list or a file path string")
	}
	steps := make([]ScriptStep, 0, len(arr))
	for i, item := range arr {
		st, err := normalizeStep(item, i)
		if err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	return &StepsOrFile{Steps: steps}, nil
}

func normalizeHandleCondition(raw any) (*HandleCondition, error) {
	m, ok := asMap(raw)
	if !ok {
		if raw == nil {
			return nil, newParseError("handle: when must be an object")
		}
		return &HandleCondition{}, nil
	}
	cond := &HandleCondition{}
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
	return cond, nil
}

func normalizeHandle(value any) (ScriptStep, error) {
	arr, ok := asSlice(value)
	if !ok {
		return nil, newParseError("handle: expected an array of branches")
	}
	branches := make([]HandleBranch, 0, len(arr))
	var otherwise *string
	for _, raw := range arr {
		entry, ok := asMap(raw)
		if !ok {
			return nil, newParseError("handle: expected each branch to be an object")
		}
		if v, present := entry["otherwise"]; present {
			s := "fail"
			if sv, ok := asString(v); ok && sv == "skip" {
				s = "skip"
			}
			otherwise = &s
			continue
		}
		whenRaw, hasWhen := entry["when"]
		doRaw, hasDo := entry["do"]
		if hasWhen && hasDo {
			when, err := normalizeHandleCondition(whenRaw)
			if err != nil {
				return nil, err
			}
			do, err := normalizeDoValue(doRaw)
			if err != nil {
				return nil, err
			}
			branches = append(branches, HandleBranch{When: *when, Do: *do})
		}
	}
	return &HandleStep{Command: CommandHandle, Branches: branches, Otherwise: otherwise}, nil
}

func normalizeBranchMatch(raw any) (*BranchMatchCondition, error) {
	m, _ := asMap(raw)
	cond := &BranchMatchCondition{}
	if v, ok := m["status"]; ok {
		if n, ok := asInt(v); ok {
			cond.Status = &n
		}
	}
	if v, ok := m["body"]; ok {
		if om, ok := asOrderedMap(v); ok {
			cond.Body = &om
		}
	}
	return cond, nil
}

func normalizeBranch(value any) (ScriptStep, error) {
	m, _ := asMap(value)
	step := &BranchStep{Command: CommandBranch, From: stringOrDefault(m["from"], "")}
	if v, ok := m["on"]; ok {
		arr, ok := asSlice(v)
		if !ok {
			return nil, newParseError("branch.on: expected an array")
		}
		on := make([]BranchCase, 0, len(arr))
		for _, raw := range arr {
			cm, _ := asMap(raw)
			match, err := normalizeBranchMatch(cm["match"])
			if err != nil {
				return nil, err
			}
			var do StepsOrFile
			if doRaw, hasDo := cm["do"]; hasDo {
				d, err := normalizeDoValue(doRaw)
				if err != nil {
					return nil, err
				}
				do = *d
			} else {
				do = StepsOrFile{Steps: []ScriptStep{}}
			}
			on = append(on, BranchCase{Match: *match, Do: do})
		}
		step.On = on
	}
	if v, present := m["default"]; present && v != nil {
		d, err := normalizeDoValue(v)
		if err != nil {
			return nil, err
		}
		step.Default = d
	}
	return step, nil
}

func normalizeRunFlow(value any) (ScriptStep, error) {
	if s, ok := asString(value); ok {
		return &RunFlowStep{Command: CommandRunFlow, File: s}, nil
	}
	m, _ := asMap(value)
	step := &RunFlowStep{Command: CommandRunFlow, File: stringOrDefault(m["file"], "")}
	if v, ok := m["env"]; ok && v != nil {
		vm, ok := asMap(v)
		if !ok {
			return nil, newParseError("runFlow.env: expected an object")
		}
		env := make(map[string]string, len(vm))
		for k, vv := range vm {
			s, ok := asString(vv)
			if !ok {
				return nil, newParseError("runFlow.env.%s: expected a string", k)
			}
			env[k] = s
		}
		step.Env = env
	}
	return step, nil
}
