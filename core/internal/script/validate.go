package script

import (
	"fmt"
	"regexp"
)

var (
	formatRegex     = regexp.MustCompile(`^atomyx/v\d+$`)
	captureVarRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type reportFunc func(format string, args ...any)

// Validate checks def against every rule ScriptDefinitionSchema and
// ScriptStepSchema enforce at runtime: required fields, enums,
// numeric ranges, regex-constrained strings, and the pointer and
// precondition cross-field rules (the zod superRefine and refine
// equivalents). It returns one message per violation; a valid
// definition returns an empty slice.
func Validate(def *ScriptDefinition) []string {
	var errs []string
	report := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	if def.Format != nil && !formatRegex.MatchString(*def.Format) {
		report("format: must match 'atomyx/v<n>' (e.g. 'atomyx/v1'), got %q", *def.Format)
	}
	if len(def.Name) == 0 {
		report("name: must not be empty")
	}
	for i, tag := range def.Tags {
		if len(tag) == 0 {
			report("tags[%d]: must not be empty", i)
		}
	}
	if def.Proxy != nil && *def.Proxy != "required" && *def.Proxy != "optional" {
		report("proxy: must be \"required\" or \"optional\", got %q", *def.Proxy)
	}
	for i, r := range def.Requires {
		if len(r) == 0 {
			report("requires[%d]: must not be empty", i)
		}
	}
	if def.StepDelay != nil && *def.StepDelay < 0 {
		report("stepDelay: must be nonnegative")
	}
	for i, p := range def.Preconditions {
		validatePrecondition(&p, fmt.Sprintf("preconditions[%d]", i), report)
	}
	for i, step := range def.Steps {
		validateStep(step, fmt.Sprintf("steps[%d]", i), report)
	}

	return errs
}

func validateSelector(sel *ScriptSelector, path string, report reportFunc) {
	if sel == nil {
		return
	}
	if sel.Nth != nil && *sel.Nth < 0 {
		report("%s.nth: must be nonnegative", path)
	}
	validatePatternField(path+".text", sel.Text != nil, sel.TextRegex, report)
	validatePatternField(path+".id", sel.ID != nil, sel.IDRegex, report)
	validatePatternField(path+".label", sel.Label != nil, sel.LabelRegex, report)
	validatePatternField(path+".hint", sel.Hint != nil, sel.HintRegex, report)
}

// validatePatternField reports a selector field that is set as both a
// literal and a regex, and a regex that does not compile.
func validatePatternField(path string, hasLiteral bool, pattern *RegexPattern, report reportFunc) {
	if pattern == nil {
		return
	}
	if hasLiteral {
		report("%s: set either a string or a {regex, flags} object, not both", path)
	}
	if _, err := pattern.Compile(); err != nil {
		report("%s: invalid regex %q: %s", path, pattern.Regex, err.Error())
	}
}

func validateStringOrSelector(v *StringOrSelector, path string, report reportFunc) {
	if v == nil || v.Selector == nil {
		return
	}
	validateSelector(v.Selector, path, report)
}

func validatePreconditionCheck(check *PreconditionCheck, path string, report reportFunc) {
	hasVisible := check.Visible != nil
	hasNotVisible := check.NotVisible != nil
	if hasVisible == hasNotVisible {
		report("%s: exactly one of `visible` or `notVisible` must be set, not zero and not both", path)
	}
	validateStringOrSelector(check.Visible, path+".visible", report)
	validateStringOrSelector(check.NotVisible, path+".notVisible", report)
}

func validatePrecondition(p *Precondition, path string, report reportFunc) {
	validatePreconditionCheck(&p.Check, path+".check", report)
	validateStepsOrFile(p.Setup, path+".setup", report)
	if p.Policy != nil {
		switch *p.Policy {
		case "ensure", "always", "skip":
		default:
			report("%s.policy: must be \"ensure\", \"always\", or \"skip\", got %q", path, *p.Policy)
		}
	}
}

func validateStepsOrFile(v StepsOrFile, path string, report reportFunc) {
	for i, s := range v.Steps {
		validateStep(s, fmt.Sprintf("%s[%d]", path, i), report)
	}
}

func validateCaptureVar(name string, path string, report reportFunc) {
	if !captureVarRegex.MatchString(name) {
		report("%s: variable names must match /^[A-Za-z_][A-Za-z0-9_]*$/, got %q", path, name)
	}
}

func validateStep(step ScriptStep, path string, report reportFunc) {
	switch s := step.(type) {
	case *TapStep:
		validateSelector(&s.Selector, path+".selector", report)

	case *TypeStep:
		if s.Into != nil {
			validateSelector(s.Into, path+".into", report)
		}

	case *WaitForStep:
		validateSelector(&s.Selector, path+".selector", report)
		validateTimeoutMs(s.TimeoutMs, path, report)

	case *AssertVisibleStep:
		validateSelector(&s.Selector, path+".selector", report)
		validateTimeoutMs(s.TimeoutMs, path, report)

	case *AssertNotVisibleStep:
		validateSelector(&s.Selector, path+".selector", report)
		validateTimeoutMs(s.TimeoutMs, path, report)

	case *AssertExistsStep:
		validateSelector(&s.Selector, path+".selector", report)
		validateTimeoutMs(s.TimeoutMs, path, report)

	case *SwipeStep:
		switch s.Direction {
		case "up", "down", "left", "right":
		default:
			report("%s.direction: must be one of up/down/left/right, got %q", path, s.Direction)
		}

	case *PressKeyStep:
		if len(s.Key) == 0 {
			report("%s.key: must not be empty", path)
		}

	case *SleepStep:
		if s.Ms < 0 {
			report("%s.ms: must be nonnegative", path)
		}

	case *CaptureStep:
		if len(s.Pattern) == 0 {
			report("%s.pattern: must not be empty", path)
		}
		validateCaptureVar(s.As, path+".as", report)

	case *AssertApiStep:
		validateCaptureVar(s.From, path+".from", report)
		if s.Status != nil && (*s.Status < 100 || *s.Status > 599) {
			report("%s.status: must be between 100 and 599", path)
		}

	case *ExtractStep:
		validateCaptureVar(s.From, path+".from", report)

	case *ReadStep:
		validateSelector(&s.Selector, path+".selector", report)
		validateCaptureVar(s.As, path+".as", report)
		if s.Field != nil && !isValidReadField(*s.Field) {
			report("%s.field: unknown field %q", path, *s.Field)
		}

	case *AssertExprStep:
		if len(s.Expr) == 0 {
			report("%s.expr: must not be empty", path)
		}

	case *HandleStep:
		if len(s.Branches) == 0 {
			report("%s.branches: must have at least one branch", path)
		}
		if s.Otherwise != nil && *s.Otherwise != "fail" && *s.Otherwise != "skip" {
			report("%s.otherwise: must be \"fail\" or \"skip\", got %q", path, *s.Otherwise)
		}
		if s.Timeout != nil && *s.Timeout < 0 {
			report("%s.timeout: must be nonnegative", path)
		}
		for i, b := range s.Branches {
			validateStepsOrFile(b.Do, fmt.Sprintf("%s.branches[%d].do", path, i), report)
		}

	case *BranchStep:
		validateCaptureVar(s.From, path+".from", report)
		if len(s.On) == 0 {
			report("%s.on: must have at least one case", path)
		}
		for i, c := range s.On {
			if c.Match.Status != nil && (*c.Match.Status < 100 || *c.Match.Status > 599) {
				report("%s.on[%d].match.status: must be between 100 and 599", path, i)
			}
			validateStepsOrFile(c.Do, fmt.Sprintf("%s.on[%d].do", path, i), report)
		}
		if s.Default != nil {
			validateStepsOrFile(*s.Default, path+".default", report)
		}

	case *RunFlowStep:
		if len(s.File) == 0 {
			report("%s.file: must not be empty", path)
		}

	case *PointerStep:
		validatePointerStep(s, path, report)

	case *LongPressStep:
		validateLongPressStep(s, path, report)

	case *PinchStep:
		validatePinchStep(s, path, report)

	case *RotateStep:
		validateRotateStep(s, path, report)

	case *RepeatStep:
		validateRepeatStep(s, path, report)

	case *RetryStep:
		validateRetryStep(s, path, report)

	case *LaunchAppStep:
		validateLaunchAppStep(s, path, report)

	case *StopAppStep:
		validateAppID(s.AppID, path, report)

	case *KillAppStep:
		validateAppID(s.AppID, path, report)

	case *ClearStateStep:
		validateAppID(s.AppID, path, report)

	case *OpenLinkStep:
		validateOpenLinkStep(s, path, report)

	case *SetOrientationStep:
		validateSetOrientationStep(s, path, report)

	case *SetLocationStep:
		validateSetLocationStep(s, path, report)

	case *TravelStep:
		validateTravelStep(s, path, report)

	case *SetPermissionsStep:
		validateSetPermissionsStep(s, path, report)

	case *EraseTextStep:
		validateEraseTextStep(s, path, report)

	case *ScrollUntilVisibleStep:
		validateScrollUntilVisibleStep(s, path, report)

	case *WaitForAnimationToEndStep:
		validateTimeoutMs(s.TimeoutMs, path, report)

	case *InputRandomStep:
		validateInputRandomStep(s, path, report)

	case *BackStep, *ScreenshotStep, *HideKeyboardStep:
		// No per-step constraints beyond the discriminant.
	}
}

func validateTimeoutMs(timeoutMs *int, path string, report reportFunc) {
	if timeoutMs != nil && *timeoutMs < 0 {
		report("%s.timeoutMs: must be nonnegative", path)
	}
}

func validatePointerStep(s *PointerStep, path string, report reportFunc) {
	if len(s.Touches) == 0 {
		report("%s.touches: must have at least one entry", path)
	}
	for ti, t := range s.Touches {
		if len(t.Actions) == 0 {
			report("%s.touches[%d].actions: must have at least one action", path, ti)
		}
		for ai, a := range t.Actions {
			validatePointerAction(a, fmt.Sprintf("%s.touches[%d].actions[%d]", path, ti, ai), report)
		}
	}
	// A multi-pointer gesture needs a non-empty id per touch so the
	// concurrent paths can be told apart; a single-pointer gesture has
	// only one path, so its id is optional and unused.
	if len(s.Touches) > 1 {
		for ti, t := range s.Touches {
			if len(t.ID) == 0 {
				report("%s.touches[%d].id: must not be empty in a multi-pointer gesture", path, ti)
			}
		}
	}
	if s.MoveDurationMs != nil && *s.MoveDurationMs < 0 {
		report("%s.moveDurationMs: must be nonnegative", path)
	}
}

func validatePointerAction(a PointerAction, path string, report reportFunc) {
	switch a.Type {
	case "down", "move":
		if a.Target == nil {
			report("%s.target: required", path)
			return
		}
		validatePointerTarget(a.Target, path+".target", report)
		if a.Pressure != nil && (*a.Pressure < 0 || *a.Pressure > 1) {
			report("%s.pressure: must be between 0 and 1", path)
		}
	case "wait":
		if a.Ms == nil || *a.Ms < 0 {
			report("%s.ms: must be a nonnegative number", path)
		}
	case "up":
		// No fields to validate.
	case "capture":
		// No required fields; Label is optional.
	default:
		report("%s.type: unknown pointer action %q", path, a.Type)
	}
}

func validatePointerTarget(t *PointerTarget, path string, report reportFunc) {
	hasSelector := t.Selector != nil
	hasPoint := t.Point != nil
	if hasSelector == hasPoint {
		report("%s: exactly one of `selector` or `point` must be set", path)
		return
	}
	if hasSelector {
		validateSelector(t.Selector, path+".selector", report)
		if t.Anchor != nil && !isValidPointerAnchor(*t.Anchor) {
			report("%s.anchor: unknown anchor %q", path, *t.Anchor)
		}
	}
}

// validateGestureLocation checks a gesture-sugar step's location field
// (`at`, `center`, `thenSwipeTo`): required, and a well-formed target
// union of exactly one of a selector or an absolute point.
func validateGestureLocation(t *PointerTarget, path string, report reportFunc) {
	if t == nil {
		report("%s: required", path)
		return
	}
	validatePointerTarget(t, path, report)
}

func validateLongPressStep(s *LongPressStep, path string, report reportFunc) {
	validateGestureLocation(s.At, path+".at", report)
	if s.HoldMs != nil && *s.HoldMs < 0 {
		report("%s.holdMs: must be nonnegative", path)
	}
	if s.ThenSwipeTo != nil {
		validatePointerTarget(s.ThenSwipeTo, path+".thenSwipeTo", report)
	}
}

func validatePinchStep(s *PinchStep, path string, report reportFunc) {
	validateGestureLocation(s.Center, path+".center", report)
	if s.StartGap < 0 {
		report("%s.startGap: must be nonnegative", path)
	}
	if s.EndGap < 0 {
		report("%s.endGap: must be nonnegative", path)
	}
	// An equal start and end gap describes no motion, so it is not a
	// pinch; reject it rather than dispatch a zero-length two-finger
	// gesture.
	if s.StartGap == s.EndGap {
		report("%s: startGap and endGap must differ", path)
	}
	if s.DurationMs != nil && *s.DurationMs < 0 {
		report("%s.durationMs: must be nonnegative", path)
	}
	if s.Axis != nil && !isValidPinchAxis(*s.Axis) {
		report("%s.axis: must be one of vertical/horizontal, got %q", path, *s.Axis)
	}
}

func validateRotateStep(s *RotateStep, path string, report reportFunc) {
	validateGestureLocation(s.Center, path+".center", report)
	if s.Degrees == 0 {
		report("%s.degrees: must be nonzero", path)
	}
	if s.Radius != nil && *s.Radius <= 0 {
		report("%s.radius: must be positive", path)
	}
	if s.DurationMs != nil && *s.DurationMs < 0 {
		report("%s.durationMs: must be nonnegative", path)
	}
}
