package runner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
	"dev.atomyx/internal/wire"
)

func requireTyped(t *testing.T, tr *testRunner, want ...string) {
	t.Helper()
	got := typedTexts(tr)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("typed %q, want %q", got, want)
	}
}

func TestRuntimeResolve_ReadThenTypeUsesTheReadValue(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(typeTree(button("code", "482913", "0,0,10,10")))
	result := run(t, tr, "appId: com.test\n---\n"+
		"- read: { selector: { id: code }, field: text, as: otp }\n"+
		"- type: \"${otp}\"\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	requireTyped(t, tr, "482913")
}

func TestRuntimeResolve_ExtractThenTapSelector(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("row", capturedSecret, "0,200,100,250")))
	tr.capture.queue = []CapturedRequest{{Method: "POST", URL: "/login", Status: 200, Body: map[string]any{"token": capturedSecret}}}
	result := run(t, tr, "appId: com.test\nproxy: required\n---\n"+
		"- capture: \"POST /login as: r\"\n"+
		"- extract: { from: r, values: { tok: $.body.token } }\n"+
		"- tap: { text: \"${tok}\" }\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	if countCalls(tr, "Tap") != 1 {
		t.Fatalf("calls = %+v, want one tap of the element the variable names", tr.driver.Calls)
	}
}

func TestRuntimeResolve_RepeatChildSeesASiblingsVariable(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(typeTree(button("code", "77", "0,0,10,10")))
	result := run(t, tr, "appId: com.test\n---\n"+
		"- repeat:\n    times: 2\n    commands:\n"+
		"      - read: { selector: { id: code }, field: text, as: otp }\n"+
		"      - type: \"${otp}\"\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	requireTyped(t, tr, "77", "77")
}

// stepCounter replaces `back` with a command that bumps the variable n,
// so a retried block observes a different value on each attempt.
type stepCounter struct{ attempts int }

func (*stepCounter) Command() string { return script.CommandBack }

func (c *stepCounter) Execute(_ script.ScriptStep, ctx *CommandContext) CommandResult {
	c.attempts++
	ctx.Variables["n"] = expression.StringValue(fmt.Sprint(c.attempts))
	return CommandResult{Ok: true}
}

func TestRuntimeResolve_RetryResolvesAgainOnEveryAttempt(t *testing.T) {
	counter := &stepCounter{}
	tr := newTestRunner(counter)
	tr.driver.StageHierarchy(rootTree(button("b", "3", "0,0,10,10")))
	result := run(t, tr, "appId: com.test\n---\n"+
		"- retry:\n    maxRetries: 3\n    commands:\n"+
		"      - back\n"+
		"      - assertVisible: { text: \"${n}\", timeout: 1 }\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	if counter.attempts != 3 {
		t.Fatalf("attempts = %d, want the block to pass on the third attempt", counter.attempts)
	}
}

func TestRuntimeResolve_AValueHoldingAPlaceholderIsTypedAsText(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(typeTree(button("code", "${other}", "0,0,10,10")))
	result := run(t, tr, "appId: com.test\nenv:\n  other: expanded\n---\n"+
		"- read: { selector: { id: code }, field: text, as: otp }\n"+
		"- type: \"${otp}\"\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	requireTyped(t, tr, "${other}")
}

func TestRuntimeResolve_ANameWithoutAVariableStaysLiteral(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(typeTree())
	result := run(t, tr, "appId: com.test\n---\n- type: \"x-${nothing}\"\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	requireTyped(t, tr, "x-${nothing}")
}

func TestRuntimeResolve_ExpressionSourceIsNeverSpliced(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(typeTree(button("code", "1 == 1", "0,0,10,10")))
	result := run(t, tr, "appId: com.test\n---\n"+
		"- read: { selector: { id: code }, field: text, as: v }\n"+
		"- assertExpr: { expr: \"v == '${v}'\" }\n")
	if result.Ok {
		t.Fatal("an expression must read variables directly, not take their text as source")
	}
}

func TestRuntimeResolve_AnInvalidResolvedStepFailsNamingNoValue(t *testing.T) {
	const bad = "not a link \x7f"
	tr := newTestRunner(&valueSetter{name: "v", value: bad})
	tr.driver.StageHierarchy(rootTree())
	result := run(t, tr, "appId: com.test\n---\n- back\n- openLink: \"${v}\"\n")
	if result.Ok {
		t.Fatal("an invalid link ran")
	}
	text := detail(lastStep(t, result))
	if strings.Contains(text, bad) || !strings.Contains(text, "${v}") {
		t.Fatalf("detail = %q, want the placeholder name and no value", text)
	}
	if result.FailedAtStep == nil || *result.FailedAtStep != 1 {
		t.Fatalf("failed at %v, want step 1", result.FailedAtStep)
	}
}

// valueSetter replaces `back` with a command that stores a fixed value.
type valueSetter struct{ name, value string }

func (*valueSetter) Command() string { return script.CommandBack }

func (v *valueSetter) Execute(_ script.ScriptStep, ctx *CommandContext) CommandResult {
	ctx.Variables[v.name] = expression.StringValue(v.value)
	return CommandResult{Ok: true}
}

func TestRuntimeResolve_APausedStepResolvesAgainstTheRunVariables(t *testing.T) {
	var result CommandResult
	gate := &funcGate{fn: func(s GateStep, run PausedRun) error {
		if s.StepIndex != 1 {
			return nil
		}
		step := mustParse(t, "appId: com.test\n---\n- type: \"${seed}\"\n").Steps[0]
		result, _ = run.ExecuteStep(context.Background(), step)
		return nil
	}}
	tr := gatedRunner(gate, "")
	tr.driver.StageHierarchy(typeTree())
	def := mustParse(t, "appId: com.test\nenv:\n  seed: one\n---\n- screenshot: a\n- screenshot: b\n")
	if _, err := tr.runner.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("paused step failed: %q", ptrText(result.Detail))
	}
	requireTyped(t, tr, "one")
}

func TestRuntimeResolve_AFlowFileIsNotResolvedAgain(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(typeTree(button("code", "55", "0,0,10,10")))
	tr.loader.stage("/flows/inner.yaml", "- read: { selector: { id: code }, field: text, as: late }\n- type: \"${late}\"\n")
	result := run(t, tr, "appId: com.test\n---\n- runFlow: /flows/inner.yaml\n")
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	requireTyped(t, tr, "${late}")
}

func TestRuntimeResolve_FastPathLeavesAStepUntouched(t *testing.T) {
	def := mustParse(t, "appId: com.test\n---\n- type: plain\n")
	rs := &runState{variables: map[string]ScriptValue{}}
	step := def.Steps[0]
	allocs := testing.AllocsPerRun(100, func() {
		got, err := resolveForRun(rs, step)
		if err != nil || got != step {
			t.Fatal("a step with nothing to resolve changed")
		}
	})
	if allocs != 0 {
		t.Fatalf("%v allocations on the fast path, want 0", allocs)
	}
}

// typeTree stages children beside a focused field, which a `type` step
// without a target needs to find.
func typeTree(children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return rootTree(append(children, focusedButton("field", "", "0,300,300,350"))...)
}
