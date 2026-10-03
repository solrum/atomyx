package script

import "testing"

const secretValue = "s3cr3t-value"

var secretEnv = map[string]string{"SECRET": secretValue, "APP": "com.example.secret"}

func TestAuthoredOfKeepsTheTextBeforeInterpolation(t *testing.T) {
	def, err := ParseScript("- assertExpr: { expr: \"token == '${SECRET}'\" }\n- tap: \"${SECRET}\"\n- back\n", secretEnv)
	if err != nil {
		t.Fatal(err)
	}

	expr, ok := AuthoredOf(def.Steps[0]).(*AssertExprStep)
	if !ok || expr.Expr != "token == '${SECRET}'" {
		t.Fatalf("authored expr = %+v, want the text with its placeholder", AuthoredOf(def.Steps[0]))
	}
	if got := def.Steps[0].(*AssertExprStep).Expr; got != "token == '"+secretValue+"'" {
		t.Fatalf("the executed expr = %q, want the interpolated text", got)
	}

	tap, ok := AuthoredOf(def.Steps[1]).(*TapStep)
	if !ok || tap.Selector.Text == nil || *tap.Selector.Text != "${SECRET}" {
		t.Fatalf("authored selector = %+v, want ${SECRET}", AuthoredOf(def.Steps[1]))
	}
	if *def.Steps[1].(*TapStep).Selector.Text != secretValue {
		t.Fatal("the executed selector lost its interpolated value")
	}
}

func TestAuthoredOfIsTheStepItselfWithoutPlaceholders(t *testing.T) {
	def, err := ParseScript("- tap: Login\n- back\n", secretEnv)
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range def.Steps {
		if AuthoredOf(step) != step {
			t.Fatalf("step %d: a step with no placeholder reports another authored form", i)
		}
	}
	built := &SleepStep{Command: CommandSleep, Ms: 5}
	if AuthoredOf(built) != ScriptStep(built) {
		t.Fatal("a step built without parsing is not its own authored form")
	}
}

func TestAuthoredOfReachesStepsNestedInHandleAndBranch(t *testing.T) {
	text := "- handle:\n" +
		"    - when: { visible: \"x\" }\n" +
		"      do:\n" +
		"        - tap: \"${SECRET}\"\n" +
		"    - otherwise: skip\n" +
		"- branch:\n" +
		"    from: rsp\n" +
		"    on:\n" +
		"      - match: { status: 200 }\n" +
		"        do:\n" +
		"          - pressKey: \"${SECRET}\"\n" +
		"    default:\n" +
		"      - screenshot: \"${SECRET}\"\n"
	def, err := ParseScript(text, secretEnv)
	if err != nil {
		t.Fatal(err)
	}

	handle := def.Steps[0].(*HandleStep)
	nestedTap := AuthoredOf(handle.Branches[0].Do.Steps[0]).(*TapStep)
	if *nestedTap.Selector.Text != "${SECRET}" {
		t.Fatalf("nested handle step authored selector = %q", *nestedTap.Selector.Text)
	}
	branch := def.Steps[1].(*BranchStep)
	if got := AuthoredOf(branch.On[0].Do.Steps[0]).(*PressKeyStep).Key; got != "${SECRET}" {
		t.Fatalf("nested branch case authored key = %q", got)
	}
	if got := *AuthoredOf(branch.Default.Steps[0]).(*ScreenshotStep).Label; got != "${SECRET}" {
		t.Fatalf("nested branch default authored label = %q", got)
	}
}

func TestAuthoredAppIDKeepsThePlaceholder(t *testing.T) {
	def, err := ParseScript("appId: ${APP}\n---\n- launchApp\n", secretEnv)
	if err != nil {
		t.Fatal(err)
	}
	if def.AppID != "com.example.secret" || def.AuthoredAppID() != "${APP}" {
		t.Fatalf("AppID %q, authored %q", def.AppID, def.AuthoredAppID())
	}
	built := &ScriptDefinition{AppID: "com.example.app"}
	if built.AuthoredAppID() != "com.example.app" {
		t.Fatal("a definition built without parsing has no separate authored app id")
	}
}

func TestAttachAuthoredIgnoresAStepOfAnotherKind(t *testing.T) {
	resolved := &HandleStep{Command: CommandHandle}
	attachAuthored(resolved, &BackStep{Command: CommandBack})
	if AuthoredOf(resolved) != ScriptStep(resolved) {
		t.Fatal("a step of another kind was recorded as the authored form")
	}
}

func TestAuthoredFormIsRebuiltThroughStandInsForIdentifierSlots(t *testing.T) {
	def, err := ParseScript("- capture: \"GET /${SECRET} as: ${ALIAS}\"\n", map[string]string{"SECRET": secretValue, "ALIAS": "token"})
	if err != nil {
		t.Fatal(err)
	}
	if WrittenFormLost(def.Steps[0]) {
		t.Fatal("a capture shorthand with a placeholder alias lost its written form")
	}
	written := AuthoredOf(def.Steps[0]).(*CaptureStep)
	if written.As != "${ALIAS}" || written.Pattern != "GET /${SECRET}" {
		t.Fatalf("authored capture = %+v, want both placeholders", written)
	}
	if executed := def.Steps[0].(*CaptureStep); executed.As != "token" || executed.Pattern != "GET /"+secretValue {
		t.Fatalf("the executed capture = %+v, want the expanded values", executed)
	}
}

func TestStepWithAPlaceholderInANumberLosesItsWrittenForm(t *testing.T) {
	def, err := ParseScript("- sleep: \"${MS}\"\n- tap: \"${SECRET}\"\n", map[string]string{"MS": "5", "SECRET": secretValue})
	if err != nil {
		t.Fatal(err)
	}
	if !WrittenFormLost(def.Steps[0]) {
		t.Fatal("a sleep with a placeholder duration kept a written form")
	}
	if def.Steps[0].(*SleepStep).Ms != 5 {
		t.Fatal("the lost step no longer executes with the expanded value")
	}
	if WrittenFormLost(def.Steps[1]) {
		t.Fatal("a step with a rebuildable written form was marked lost")
	}
}

func TestNestedStepsLoseTheirWrittenFormWithTheirContainer(t *testing.T) {
	text := "- handle:\n" +
		"    - when: { visible: \"x\" }\n" +
		"      do:\n" +
		"        - tap: \"${SECRET}\"\n" +
		"        - sleep: \"${MS}\"\n" +
		"    - otherwise: skip\n"
	def, err := ParseScript(text, map[string]string{"MS": "5", "SECRET": secretValue})
	if err != nil {
		t.Fatal(err)
	}
	handle := def.Steps[0].(*HandleStep)
	if !WrittenFormLost(handle) || !WrittenFormLost(handle.Branches[0].Do.Steps[0]) {
		t.Fatal("a handle holding an unrebuildable step, or its nested step, kept a written form")
	}
}
