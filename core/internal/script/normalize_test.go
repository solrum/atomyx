package script

import "testing"

func TestNormalizeSleep_RejectsFractionalMs(t *testing.T) {
	mustFail(t, "- sleep: 500.7\n")
}

func TestNormalizeSleep_RejectsNegativeMs(t *testing.T) {
	mustFail(t, "- sleep: -1\n")
}

func TestNormalizeSleep_AcceptsIntegerMs(t *testing.T) {
	def := mustParse(t, "- sleep: 500\n")
	step, ok := def.Steps[0].(*SleepStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *SleepStep", def.Steps[0])
	}
	if step.Ms != 500 {
		t.Errorf("Ms = %d, want 500", step.Ms)
	}
}

// A malformed two-key step must resolve to the first-declared key,
// the same key a strict "use the first key" reader picks, not
// whichever recognized command name sorts first alphabetically. Here
// "tap" is declared first and "back" sorts first alphabetically, so
// the two disagree: only the fixed resolution order produces a
// TapStep.
func TestNormalizeStep_MultiKeyStepUsesFirstDeclaredKey(t *testing.T) {
	def := mustParse(t, "- tap: \"Login\"\n  back:\n")
	tap, ok := def.Steps[0].(*TapStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *TapStep (the first-declared key)", def.Steps[0])
	}
	if tap.Selector.Text == nil || *tap.Selector.Text != "Login" {
		t.Errorf("tap.selector.text = %v, want \"Login\"", tap.Selector.Text)
	}
}

func TestNormalizeStep_MultiKeyStepUnrecognizedFirstKeyErrors(t *testing.T) {
	mustFail(t, "- zzz: foo\n  tap: \"Login\"\n")
}

func TestNormalizePointer_RejectsNonFiniteMoveDuration(t *testing.T) {
	mustFail(t, "- pointer:\n    moveDurationMs: .nan\n    touches:\n      - actions: [up]\n")
	mustFail(t, "- pointer:\n    moveDurationMs: .inf\n    touches:\n      - actions: [up]\n")
}

func TestNormalizePointer_RejectsNonFinitePressure(t *testing.T) {
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2, pressure: .nan }\n          - up\n")
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2, pressure: .inf }\n          - up\n")
}

func TestNormalizePointer_RejectsNonFiniteWaitMs(t *testing.T) {
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - wait: .nan\n          - up\n")
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - wait: .inf\n          - up\n")
}

func TestNormalizePointer_RejectsNonFiniteOffset(t *testing.T) {
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - down: { selector: \"Item\", offset: { dx: .nan, dy: 0 } }\n          - up\n")
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - down: { selector: \"Item\", offset: { dx: 0, dy: .inf } }\n          - up\n")
}

func TestNormalizePointer_AcceptsBareCapture(t *testing.T) {
	def := mustParse(t, "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2 }\n          - capture\n          - up\n")
	step, ok := def.Steps[0].(*PointerStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *PointerStep", def.Steps[0])
	}
	actions := step.Touches[0].Actions
	if len(actions) != 3 || actions[1].Type != "capture" {
		t.Fatalf("actions = %+v, want a bare capture at index 1", actions)
	}
	if actions[1].Label != nil {
		t.Fatalf("Label = %v, want nil for a bare capture", actions[1].Label)
	}
}

func TestNormalizePointer_AcceptsLabeledCapture(t *testing.T) {
	def := mustParse(t, "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2 }\n          - capture: mid-drag\n          - up\n")
	step, ok := def.Steps[0].(*PointerStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *PointerStep", def.Steps[0])
	}
	actions := step.Touches[0].Actions
	if actions[1].Label == nil || *actions[1].Label != "mid-drag" {
		t.Fatalf("Label = %v, want \"mid-drag\"", actions[1].Label)
	}
}

func TestNormalizePointer_RejectsEmptyCaptureLabel(t *testing.T) {
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2 }\n          - capture: \"\"\n          - up\n")
}

func TestNormalizePointer_RejectsNonStringCaptureValue(t *testing.T) {
	mustFail(t, "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2 }\n          - capture: 5\n          - up\n")
}
