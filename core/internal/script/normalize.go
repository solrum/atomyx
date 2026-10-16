package script

import (
	"errors"
	"sort"
)

// Top-level step normalization: dispatching a raw YAML step to its
// command-specific normalizer.

// stepNormalizers is the command-name to step-builder dispatch table.
// Adding a new step kind means adding one entry here plus its struct
// in steps.go. Nothing else in this file changes. Populated by
// init() rather than a var initializer: several normalizers
// (handle, branch, precondition setup) recurse back into
// normalizeStep, which reads this map, and a var initializer
// expression referring to that cycle is rejected by the compiler as
// an initialization cycle even though no call happens at init time.
var stepNormalizers map[string]func(any) (ScriptStep, error)

func init() {
	stepNormalizers = map[string]func(any) (ScriptStep, error){
		CommandLaunchApp:        normalizeLaunchApp,
		CommandTap:              normalizeTap,
		CommandType:             normalizeType,
		CommandWaitFor:          normalizeWaitFor,
		CommandAssertVisible:    normalizeAssertVisible,
		CommandAssertNotVisible: normalizeAssertNotVisible,
		CommandAssertExists:     normalizeAssertExists,
		CommandScreenshot:       normalizeScreenshot,
		CommandSwipe:            normalizeSwipe,
		CommandPressKey:         normalizePressKey,
		CommandBack:             normalizeBack,
		CommandSleep:            normalizeSleep,
		CommandCapture:          normalizeCapture,
		CommandAssertApi:        normalizeAssertApi,
		CommandExtract:          normalizeExtract,
		CommandRead:             normalizeRead,
		CommandAssertExpr:       normalizeAssertExpr,
		CommandHandle:           normalizeHandle,
		CommandBranch:           normalizeBranch,
		CommandRunFlow:          normalizeRunFlow,
		CommandPointer:          normalizePointer,
		CommandLongPress:        normalizeLongPress,
		CommandPinch:            normalizePinch,
		CommandRotate:           normalizeRotate,
		CommandRepeat:           normalizeRepeat,
		CommandRetry:            normalizeRetry,
		CommandStopApp:          normalizeStopApp,
		CommandKillApp:          normalizeKillApp,
		CommandClearState:       normalizeClearState,
		CommandOpenLink:         normalizeOpenLink,
		CommandSetPermissions:   normalizeSetPermissions,

		CommandHideKeyboard:          normalizeHideKeyboard,
		CommandEraseText:             normalizeEraseText,
		CommandScrollUntilVisible:    normalizeScrollUntilVisible,
		CommandWaitForAnimationToEnd: normalizeWaitForAnimationToEnd,
		CommandInputRandom:           normalizeInputRandom,
	}
}

// normalizeStep normalizes a raw YAML step (bare string or object)
// into a canonical ScriptStep, expanding shorthand through the registered
// step normalizers. stepIndex is 0-based and only
// used to number error messages.
func normalizeStep(raw any, stepIndex int) (ScriptStep, error) {
	if s, ok := asString(raw); ok {
		normalizer, found := stepNormalizers[s]
		if !found {
			return nil, newParseError("Step %d: unknown command %q", stepIndex+1, s)
		}
		return normalizer(nil)
	}

	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("Step %d: expected string or object, got %s", stepIndex+1, goType(raw))
	}
	if len(m) == 0 {
		return nil, newParseError("Step %d: empty step object", stepIndex+1)
	}

	commandKey := firstStepKey(raw, m)
	normalizer, found := stepNormalizers[commandKey]
	if !found {
		return nil, newParseError("Step %d: unknown command %q", stepIndex+1, commandKey)
	}
	step, err := normalizer(m[commandKey])
	var ev *extractValuesError
	if errors.As(err, &ev) {
		return nil, renderExtractValuesError(stepIndex, ev)
	}
	return step, err
}

// firstStepKey picks a step object's command key. A well-formed step
// carries exactly one key, but a malformed multi-key step must still
// resolve to exactly one command, the same one a strict "use the
// first declared key" reader would pick. raw is an OrderedMap for
// every step reached through a script's step list, which preserves
// declaration order at every depth, so that case uses the literal
// first key: an unrecognized first key is left to the caller's
// existing unknown-command error, rather than silently falling
// through to a different, recognized key later in the object. A step
// embedded in a precondition's inline setup block decodes through
// the plain, order-losing config mapping instead; for that case
// order is unrecoverable, so the key is chosen deterministically:
// prefer a recognized command name, lowest alphabetically, otherwise
// the lowest key overall.
func firstStepKey(raw any, m map[string]any) string {
	if om, ok := raw.(OrderedMap); ok {
		return om.Keys()[0]
	}
	if len(m) == 1 {
		for k := range m {
			return k
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := stepNormalizers[k]; ok {
			return k
		}
	}
	return keys[0]
}
