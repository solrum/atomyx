package runner

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/script/expression"
	"dev.atomyx/internal/wire"
)

// funcGate adapts a function to StepGate and records each step it sees.
type funcGate struct {
	seen []GateStep
	fn   func(step GateStep, run PausedRun) error
}

func (g *funcGate) BeforeStep(_ context.Context, step GateStep, run PausedRun) error {
	g.seen = append(g.seen, step)
	if g.fn != nil {
		return g.fn(step, run)
	}
	return nil
}

func gatedRunner(gate StepGate, sourcePath string) *testRunner {
	tr := newTestRunner()
	deps := tr.runner.deps
	deps.StepGate = gate
	deps.SourcePath = sourcePath
	tr.runner = NewScriptRunner(deps)
	return tr
}

const nestedYAML = "appId: com.test\n---\n" +
	"- launchApp\n" +
	"- launchApp\n" +
	"- handle:\n" +
	"    - when:\n" +
	"        visible: Go\n" +
	"      do:\n" +
	"        - screenshot: a\n" +
	"        - screenshot: b\n" +
	"- screenshot: last\n"

func TestStepGate_RunsBeforeEveryStepAtEveryDepthAfterTheSkipCheck(t *testing.T) {
	gate := &funcGate{}
	tr := gatedRunner(gate, "/s/flow.yaml")
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "0,0,10,10")))

	result := run(t, tr, nestedYAML)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}

	var commands []string
	var depths []int
	for _, s := range gate.seen {
		commands = append(commands, s.Command)
		depths = append(depths, s.Depth)
	}
	wantCommands := []string{"launchApp", "handle", "screenshot", "screenshot", "screenshot"}
	wantDepths := []int{0, 0, 1, 1, 0}
	if len(commands) != len(wantCommands) {
		t.Fatalf("gate saw %v at depths %v, want %v (the second launchApp is skipped and must never reach the gate)", commands, depths, wantCommands)
	}
	for i := range wantCommands {
		if commands[i] != wantCommands[i] || depths[i] != wantDepths[i] {
			t.Fatalf("gate saw %v at depths %v, want %v at %v", commands, depths, wantCommands, wantDepths)
		}
		if gate.seen[i].StepIndex != i {
			t.Fatalf("step %d got StepIndex %d, want %d", i, gate.seen[i].StepIndex, i)
		}
	}
}

func TestStepGate_RunsBeforeTheStartedEvent(t *testing.T) {
	var order []string
	gate := &funcGate{fn: func(GateStep, PausedRun) error {
		order = append(order, "gate")
		return nil
	}}
	tr := gatedRunner(gate, "")
	deps := tr.runner.deps
	deps.OnStep = func(e StepEvent) {
		if e.Type == StepStarted {
			order = append(order, "started")
		}
	}
	tr.runner = NewScriptRunner(deps)

	run(t, tr, "appId: com.test\n---\n- screenshot: a\n")
	if len(order) != 2 || order[0] != "gate" || order[1] != "started" {
		t.Fatalf("got order %v, want [gate started]", order)
	}
}

func TestStepGate_LocationIsSourceAndLineForTopLevelStepsOnly(t *testing.T) {
	gate := &funcGate{}
	tr := gatedRunner(gate, "/s/flow.yaml")
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "0,0,10,10")))

	def := mustParse(t, nestedYAML)
	if _, err := tr.runner.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}
	topLevel := []int{0, 2, 3}
	next := 0
	for _, s := range gate.seen {
		if s.Depth > 0 {
			if s.Location != "" {
				t.Fatalf("nested step has location %q, want empty", s.Location)
			}
			continue
		}
		want := "/s/flow.yaml:" + strconv.Itoa(def.StepLines[topLevel[next]])
		next++
		if s.Location != want {
			t.Fatalf("got location %q, want %q", s.Location, want)
		}
	}
}

func TestStepGate_InlineScriptLocationUsesInlinePrefix(t *testing.T) {
	gate := &funcGate{}
	tr := gatedRunner(gate, "")
	def := mustParse(t, "appId: com.test\n---\n- screenshot: a\n")
	if _, err := tr.runner.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "inline:" + strconv.Itoa(def.StepLines[0]); gate.seen[0].Location != want {
		t.Fatalf("got location %q, want %q", gate.seen[0].Location, want)
	}
}

func TestStepGate_TopLevelErrorEndsTheRunAsCancelled(t *testing.T) {
	gate := &funcGate{fn: func(s GateStep, _ PausedRun) error {
		if s.StepIndex == 1 {
			return context.Canceled
		}
		return nil
	}}
	tr := gatedRunner(gate, "")
	def := mustParse(t, "appId: com.test\n---\n- screenshot: a\n- screenshot: b\n- screenshot: c\n")
	_, err := tr.runner.Run(context.Background(), def)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err %v, want context.Canceled", err)
	}
	if len(gate.seen) != 2 {
		t.Fatalf("gate saw %d steps, want 2", len(gate.seen))
	}
}

func TestStepGate_NestedErrorEndsTheRunAsCancelled(t *testing.T) {
	gate := &funcGate{fn: func(s GateStep, _ PausedRun) error {
		if s.Depth == 1 {
			return context.Canceled
		}
		return nil
	}}
	tr := gatedRunner(gate, "")
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "0,0,10,10")))
	_, err := tr.runner.Run(context.Background(), mustParse(t, nestedYAML))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err %v, want context.Canceled", err)
	}
}

func TestStepGate_NilGateLeavesRunUnchanged(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "0,0,10,10")))
	result := run(t, tr, nestedYAML)
	if !result.Ok || len(result.Steps) != 3 {
		t.Fatalf("got %s", debugSteps(result.Steps))
	}
}

func TestPausedRun_ExecuteStepDiscardsWritesAndLeavesTheRunUnchanged(t *testing.T) {
	var stepResult CommandResult
	var execErr error
	var insideBefore, insideAfter VariableSnapshot
	gate := &funcGate{fn: func(s GateStep, run PausedRun) error {
		if s.StepIndex != 1 {
			return nil
		}
		insideBefore = run.Variables()
		readStep := mustParse(t, "appId: com.test\n---\n- read:\n    selector:\n      id: field1\n    field: value\n    as: fromAct\n").Steps[0]
		stepResult, execErr = run.ExecuteStep(context.Background(), readStep)
		insideAfter = run.Variables()
		return nil
	}}
	tr := gatedRunner(gate, "")
	tr.driver.StageHierarchy(rootTree(wire.TreeNodeWire{
		Attributes: map[string]string{"id": "field1", "value": "hello", "bounds": "0,0,10,10"},
	}))
	def := mustParse(t, "appId: com.test\nenv:\n  seed: one\n---\n- screenshot: a\n- screenshot: b\n")
	if _, err := tr.runner.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if execErr != nil || !stepResult.Ok {
		t.Fatalf("ExecuteStep got ok=%t err=%v detail=%q", stepResult.Ok, execErr, ptrText(stepResult.Detail))
	}
	if v, ok := insideBefore.Values["seed"]; !ok || v.Str() != "one" {
		t.Fatalf("snapshot lost the seeded variable: %+v", insideBefore.Values)
	}
	if _, leaked := insideAfter.Values["fromAct"]; leaked {
		t.Fatalf("a variable written by ExecuteStep reached the run: %+v", insideAfter.Values)
	}
}

func TestPausedRun_ExecuteStepReadsRunVariables(t *testing.T) {
	var result CommandResult
	gate := &funcGate{fn: func(s GateStep, run PausedRun) error {
		if s.StepIndex != 1 {
			return nil
		}
		step := mustParse(t, "appId: com.test\n---\n- assertExpr:\n    expr: seed == \"one\"\n").Steps[0]
		result, _ = run.ExecuteStep(context.Background(), step)
		return nil
	}}
	tr := gatedRunner(gate, "")
	def := mustParse(t, "appId: com.test\nenv:\n  seed: one\n---\n- screenshot: a\n- screenshot: b\n")
	if _, err := tr.runner.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("child scope did not see the run variables: %q", ptrText(result.Detail))
	}
}

func TestPausedRun_ExecuteStepRefusesNestingCommands(t *testing.T) {
	cases := map[string]string{
		"handle":  "- handle:\n    - when:\n        visible: Go\n      do:\n        - screenshot: a\n",
		"runFlow": "- runFlow: other.yaml\n",
		"branch":  "- branch:\n    from: r\n    on:\n      - match:\n          status: 200\n        do:\n          - screenshot: a\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var execErr error
			gate := &funcGate{fn: func(_ GateStep, run PausedRun) error {
				step := mustParse(t, "appId: com.test\n---\n"+body).Steps[0]
				_, execErr = run.ExecuteStep(context.Background(), step)
				return nil
			}}
			tr := gatedRunner(gate, "")
			run(t, tr, "appId: com.test\n---\n- screenshot: a\n")
			var coded diag.Coded
			if !errors.As(execErr, &coded) || coded.Code() != diag.CodeInvalidArgument {
				t.Fatalf("got %v, want an INVALID_ARGUMENT error", execErr)
			}
		})
	}
}

func TestPausedRun_ExecuteStepAfterTheGateReturnsIsRefused(t *testing.T) {
	var kept PausedRun
	gate := &funcGate{fn: func(_ GateStep, run PausedRun) error {
		kept = run
		return nil
	}}
	tr := gatedRunner(gate, "")
	run(t, tr, "appId: com.test\n---\n- screenshot: a\n")
	step := mustParse(t, "appId: com.test\n---\n- screenshot: a\n").Steps[0]
	if _, err := kept.ExecuteStep(context.Background(), step); !errors.Is(err, ErrPausedRunClosed) {
		t.Fatalf("got %v, want ErrPausedRunClosed", err)
	}
}

func TestPausedRun_ExecuteStepTouchesNoRunBookkeeping(t *testing.T) {
	var events []StepEvent
	gate := &funcGate{fn: func(s GateStep, run PausedRun) error {
		if s.StepIndex == 0 {
			step := mustParse(t, "appId: com.test\n---\n- screenshot: x\n").Steps[0]
			_, _ = run.ExecuteStep(context.Background(), step)
		}
		return nil
	}}
	tr := gatedRunner(gate, "")
	deps := tr.runner.deps
	deps.OnStep = func(e StepEvent) { events = append(events, e) }
	tr.runner = NewScriptRunner(deps)
	result := run(t, tr, "appId: com.test\n---\n- screenshot: a\n- screenshot: b\n")
	if len(result.Steps) != 2 || len(events) != 4 {
		t.Fatalf("got %d results and %d events, want 2 and 4", len(result.Steps), len(events))
	}
	for i, e := range events {
		if want := i / 2; e.StepIndex != want {
			t.Fatalf("event %d has StepIndex %d, want %d", i, e.StepIndex, want)
		}
	}
}

func TestVariableSnapshot_CopiesValues(t *testing.T) {
	src := map[string]ScriptValue{"a": expression.StringValue("one")}
	snap := snapshotVariables(src, nil)
	snap.Values["a"] = expression.StringValue("changed")
	snap.Values["b"] = expression.StringValue("new")
	if src["a"].Str() != "one" || len(src) != 1 {
		t.Fatalf("mutating the snapshot reached the source: %+v", src)
	}
}

func ptrText(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestStepGate_ReceivesTheStepAboutToRun(t *testing.T) {
	gate := &funcGate{}
	tr := gatedRunner(gate, "/s/flow.yaml")
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "0,0,10,10")))

	run(t, tr, nestedYAML)
	if len(gate.seen) == 0 {
		t.Fatal("the gate saw no step")
	}
	for _, s := range gate.seen {
		if s.Step == nil || s.Step.StepCommand() != s.Command {
			t.Fatalf("gate step %+v does not carry the step it describes", s)
		}
	}
}
