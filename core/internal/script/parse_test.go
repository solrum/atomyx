package script

import (
	"fmt"
	"testing"
)

func mustParse(t *testing.T, yamlContent string) *ScriptDefinition {
	t.Helper()
	def, err := ParseScript(yamlContent, nil)
	if err != nil {
		t.Fatalf("ParseScript(%q) unexpected error: %v", yamlContent, err)
	}
	return def
}

func mustFail(t *testing.T, yamlContent string) {
	t.Helper()
	if def, err := ParseScript(yamlContent, nil); err == nil {
		t.Fatalf("ParseScript(%q) expected an error, got %+v", yamlContent, def)
	}
}

func TestParseScript_FlowFragment(t *testing.T) {
	def := mustParse(t, "- launchApp\n- tap: \"Login\"\n")
	if len(def.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(def.Steps))
	}
	if def.Steps[0].StepCommand() != CommandLaunchApp {
		t.Errorf("step 0 command = %q, want launchApp", def.Steps[0].StepCommand())
	}
	tap, ok := def.Steps[1].(*TapStep)
	if !ok {
		t.Fatalf("step 1 is %T, want *TapStep", def.Steps[1])
	}
	if tap.Selector.Text == nil || *tap.Selector.Text != "Login" {
		t.Errorf("tap.selector.text = %v, want \"Login\"", tap.Selector.Text)
	}
	if tap.Selector.Label == nil || *tap.Selector.Label != "Login" {
		t.Errorf("tap.selector.label = %v, want \"Login\" (mirrored from text)", tap.Selector.Label)
	}
	if *def.Format != ScriptDefaults.Format {
		t.Errorf("format = %q, want default %q", *def.Format, ScriptDefaults.Format)
	}
}

func TestParseScript_EntryScriptWithEnvInterpolation(t *testing.T) {
	yamlContent := "appId: com.example.app\n" +
		"name: Login flow\n" +
		"env:\n" +
		"  email: user@test.com\n" +
		"---\n" +
		"- launchApp\n" +
		"- type:\n" +
		"    into: { id: email }\n" +
		"    text: ${email}\n"
	def := mustParse(t, yamlContent)
	if def.AppID != "com.example.app" {
		t.Errorf("appId = %q", def.AppID)
	}
	if def.Name != "Login flow" {
		t.Errorf("name = %q", def.Name)
	}
	typeStep, ok := def.Steps[1].(*TypeStep)
	if !ok {
		t.Fatalf("step 1 is %T, want *TypeStep", def.Steps[1])
	}
	if typeStep.Text != "user@test.com" {
		t.Errorf("type.text = %q, want interpolated env value", typeStep.Text)
	}
	if len(def.StepLines) != 2 || def.StepLines[0] == 0 || def.StepLines[1] == 0 {
		t.Errorf("_stepLines = %v, want two nonzero line numbers", def.StepLines)
	}
}

func TestParseScript_SingleDocWithStepsKey(t *testing.T) {
	def := mustParse(t, "appId: com.example.app\nname: Smoke\nsteps:\n  - launchApp\n  - back\n")
	if len(def.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(def.Steps))
	}
}

func TestParseScript_AllStepKinds(t *testing.T) {
	yamlContent := `
appId: com.example.app
name: All step kinds
env:
  otpCode: "123456"
---
- launchApp
- tap: "Login"
- type: { into: { id: email }, text: hi }
- waitFor: { text: "Home", timeout: 5000 }
- assertVisible: "Home"
- assertNotVisible: "Loading"
- screenshot: home
- swipe: up
- pressKey: enter
- back
- sleep: 100
- capture: "POST /api/login as: loginRsp"
- assertApi: { from: loginRsp, status: 200, body: { "$.ok": true } }
- extract: { from: loginRsp, values: { token: $.token } }
- read: { selector: { id: qty }, field: value, as: qty }
- assertExpr: { expr: "qty == 5" }
- handle:
    - when: { visible: "OTP" }
      do:
        - tap: "Verify"
    - otherwise: skip
- branch:
    from: loginRsp
    on:
      - match: { status: 200 }
        do:
          - tap: "Dashboard"
    default:
      - screenshot: unexpected
- runFlow: flows/cleanup.yml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 100
          - up
`
	def := mustParse(t, yamlContent)
	if len(def.Steps) != 20 {
		t.Fatalf("expected 20 steps, got %d", len(def.Steps))
	}
	if errs := Validate(def); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestParseScript_PointerTouches(t *testing.T) {
	missingTouches := "- pointer:\n    moveDurationMs: 100\n"
	mustFail(t, missingTouches)

	emptyTouches := "- pointer:\n    touches: []\n"
	mustFail(t, emptyTouches)

	singleTouch := "- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 2 }\n          - up\n"
	mustParse(t, singleTouch)

	multiTouch := "- pointer:\n    touches:\n      - id: f1\n        actions:\n          - down: { x: 1, y: 2 }\n          - up\n      - id: f2\n        actions:\n          - down: { x: 3, y: 4 }\n          - up\n"
	mustParse(t, multiTouch)
}

func TestParseScript_PreconditionCheckRefine(t *testing.T) {
	base := "appId: com.example.app\nname: Smoke\nenv: {}\npreconditions:\n  - check: {%s}\n    setup: flows/setup.yml\n---\n- launchApp\n"

	mustFail(t, fmt.Sprintf(base, ""))
	mustFail(t, fmt.Sprintf(base, `visible: "Home", notVisible: "Onboarding"`))
	mustParse(t, fmt.Sprintf(base, `visible: "Home"`))
}

func TestParseScript_UnknownCommand(t *testing.T) {
	mustFail(t, "- bogusCommand\n")
}

func TestParseScript_EmptyScript(t *testing.T) {
	mustFail(t, "")
}

func TestParseScript_NoStepsNoAppId(t *testing.T) {
	mustFail(t, "appId: \"\"\nname: Empty\nenv: {}\nsteps: []\n")
}

func TestParseScript_InvalidFormat(t *testing.T) {
	mustFail(t, "format: v1\nappId: com.example.app\nname: Smoke\nenv: {}\n---\n- launchApp\n")
}

func TestParseScript_CaptureVarRegex(t *testing.T) {
	mustFail(t, "- capture: { pattern: \"POST /x\", as: \"1bad\" }\n")
	mustParse(t, "- capture: { pattern: \"POST /x\", as: \"good_name\" }\n")
}

func TestParseScript_AssertApiStatusRange(t *testing.T) {
	mustFail(t, "- assertApi: { from: x, status: 999 }\n")
	mustParse(t, "- assertApi: { from: x, status: 200 }\n")
}

// TestParseScript_HandleConditionNotEnforced pins the current, faithful
// behavior: unlike a precondition check, a handle branch's `when` with
// both `visible` and `notVisible` set, or with neither set, is
// accepted, because HandleConditionSchema carries no zod `.refine` the
// way PreconditionCheckSchema does. A future change that starts
// rejecting either shape here without an equivalent zod `.refine`
// landing first would silently diverge from the TypeScript runtime.
func TestParseScript_HandleConditionNotEnforced(t *testing.T) {
	both := "- handle:\n" +
		"    - when: { visible: \"A\", notVisible: \"B\" }\n" +
		"      do:\n" +
		"        - back\n" +
		"    - otherwise: skip\n"
	def := mustParse(t, both)
	handle, ok := def.Steps[0].(*HandleStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *HandleStep", def.Steps[0])
	}
	when := handle.Branches[0].When
	if when.Visible == nil || when.NotVisible == nil {
		t.Fatalf("expected both `visible` and `notVisible` to survive parsing, got %+v", when)
	}

	neither := "- handle:\n" +
		"    - when: {}\n" +
		"      do:\n" +
		"        - back\n" +
		"    - otherwise: skip\n"
	def = mustParse(t, neither)
	handle, ok = def.Steps[0].(*HandleStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *HandleStep", def.Steps[0])
	}
	when = handle.Branches[0].When
	if when.Visible != nil || when.NotVisible != nil {
		t.Fatalf("expected neither `visible` nor `notVisible` to be set, got %+v", when)
	}
	if errs := Validate(def); len(errs) != 0 {
		t.Fatalf("expected no validation errors for an empty handle condition, got %v", errs)
	}
}
