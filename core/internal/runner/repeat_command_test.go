package runner

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/script"
)

// countCalls returns how many times the mock driver recorded method.
func countCalls(tr *testRunner, method string) int {
	n := 0
	for _, c := range tr.driver.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func TestRepeatCommand_TimesRunsTheBlockThatManyTimes(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 3\n    commands:\n      - screenshot: shot\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := len(result.Artifacts.Screenshots()); got != 3 {
		t.Errorf("got %d screenshots, want 3", got)
	}
	if got := detail(lastStep(t, result)); got != "repeat: completed 3 iterations" {
		t.Errorf("got detail %q", got)
	}
}

func TestRepeatCommand_WhileEndsWhenTheConditionStopsHolding(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchyRepeated(rootTree(button("more", "More", "0,0,10,10")), 2)
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    while:\n      visible: More\n    commands:\n      - screenshot: shot\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := len(result.Artifacts.Screenshots()); got != 2 {
		t.Errorf("got %d iterations, want 2", got)
	}
	if got := detail(lastStep(t, result)); got != "repeat: condition ended the loop after 2 iterations" {
		t.Errorf("got detail %q", got)
	}
}

func TestRepeatCommand_NotVisibleCondition(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchyRepeated(rootTree(), 1)
	tr.driver.StageHierarchy(rootTree(button("done", "Done", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    while:\n      notVisible: Done\n    commands:\n      - screenshot: shot\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := len(result.Artifacts.Screenshots()); got != 1 {
		t.Errorf("got %d iterations, want 1", got)
	}
}

func TestRepeatCommand_WhileExprReadsRuntimeVariables(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("count", "x", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\nenv:\n  go: \"yes\"\n---\n"+
		"- repeat:\n    while:\n      expr: go == \"yes\"\n    times: 4\n    commands:\n      - screenshot: shot\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := len(result.Artifacts.Screenshots()); got != 4 {
		t.Errorf("got %d iterations, want 4 (times bound the loop)", got)
	}
}

func TestRepeatCommand_ExprFalseSkipsTheBlock(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\nenv:\n  go: \"no\"\n---\n"+
		"- repeat:\n    while:\n      expr: go == \"yes\"\n    commands:\n      - screenshot: shot\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := len(result.Artifacts.Screenshots()); got != 0 {
		t.Errorf("got %d iterations, want 0", got)
	}
	if got := detail(lastStep(t, result)); got != "repeat: condition ended the loop after 0 iterations" {
		t.Errorf("got detail %q", got)
	}
}

func TestRepeatCommand_WhileWithoutTimesFailsAtTheCap(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("more", "More", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    while:\n      visible: More\n    commands:\n      - screenshot: shot\n")
	if result.Ok {
		t.Fatalf("a loop whose condition never ends must fail")
	}
	if got := len(result.Artifacts.Screenshots()); got != script.RepeatWhileIterationLimit {
		t.Errorf("got %d iterations, want the %d iteration limit", got, script.RepeatWhileIterationLimit)
	}
	want := "repeat: condition still holds after 50 iterations"
	if got := detail(lastStep(t, result)); !strings.Contains(got, want) {
		t.Errorf("got detail %q, want it to contain %q", got, want)
	}
}

func TestRepeatCommand_TimesBeyondTheWhileCapIsNotCapped(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("more", "More", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 60\n    while:\n      visible: More\n    commands:\n      - back\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := countCalls(tr, "PressKey"); got != 60 {
		t.Errorf("got %d iterations, want 60", got)
	}
}

func TestRepeatCommand_BothKeysStopAtWhicheverComesFirst(t *testing.T) {
	t.Run("count first", func(t *testing.T) {
		tr := newTestRunner()
		tr.driver.StageHierarchy(rootTree(button("more", "More", "0,0,10,10")))
		result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 2\n    while:\n      visible: More\n    commands:\n      - screenshot: shot\n")
		if !result.Ok || len(result.Artifacts.Screenshots()) != 2 {
			t.Fatalf("got ok=%t screenshots=%d: %s", result.Ok, len(result.Artifacts.Screenshots()), debugSteps(result.Steps))
		}
	})
	t.Run("condition first", func(t *testing.T) {
		tr := newTestRunner()
		tr.driver.StageHierarchy(rootTree(button("more", "More", "0,0,10,10")))
		tr.driver.StageHierarchy(rootTree())
		result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 5\n    while:\n      visible: More\n    commands:\n      - screenshot: shot\n")
		if !result.Ok || len(result.Artifacts.Screenshots()) != 1 {
			t.Fatalf("got ok=%t screenshots=%d: %s", result.Ok, len(result.Artifacts.Screenshots()), debugSteps(result.Steps))
		}
	})
}

func TestRepeatCommand_NestedFailureStopsTheLoopAndNamesTheIteration(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("ok", "Ready", "0,0,10,10")))
	tr.driver.StageHierarchy(rootTree())

	var events []StepEvent
	tr.runner.deps.OnStep = func(e StepEvent) { events = append(events, e) }

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 4\n    commands:\n      - assertVisible: Ready\n      - back\n")
	if result.Ok {
		t.Fatalf("a failing nested step must fail the loop")
	}
	got := detail(lastStep(t, result))
	if !strings.HasPrefix(got, "repeat: iteration 2 of 4 failed: ") {
		t.Errorf("got detail %q, want it to name iteration 2 of 4", got)
	}
	if n := countCalls(tr, "PressKey"); n != 1 {
		t.Errorf("got %d back presses, want 1 (the loop stops at the failure)", n)
	}

	nested := 0
	for _, e := range events {
		if e.Type == StepCompleted && e.Depth == 1 {
			nested++
		}
	}
	if nested != 3 {
		t.Errorf("got %d nested completed events, want 3 (assert, back, failed assert)", nested)
	}
}

func TestRepeatCommand_UnevaluableConditionFails(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    while:\n      expr: missing_variable == 1\n    commands:\n      - back\n")
	if result.Ok {
		t.Fatalf("an unevaluable condition must fail")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "repeat: cannot evaluate condition \"missing_variable == 1\"") {
		t.Errorf("got detail %q", got)
	}
}

func TestRepeatCommand_NonBooleanConditionFails(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\nenv:\n  n: \"1\"\n---\n- repeat:\n    while:\n      expr: n\n    commands:\n      - back\n")
	if result.Ok {
		t.Fatalf("a non-boolean condition must fail")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "must evaluate to a boolean, got string") {
		t.Errorf("got detail %q", got)
	}
}

func TestRepeatCommand_StopsWhenTheRunContextIsCanceled(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := 0
	tr.runner.deps.OnStep = func(e StepEvent) {
		if e.Type == StepCompleted && e.Depth == 1 {
			completed++
			if completed == 2 {
				cancel()
			}
		}
	}

	def := mustParse(t, "appId: com.test\n---\n- repeat:\n    times: 1000\n    commands:\n      - back\n")
	result, err := tr.runner.Run(ctx, def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatalf("a canceled run must not report success")
	}
	if got := detail(lastStep(t, result)); !strings.HasPrefix(got, "Interrupted:") {
		t.Errorf("got detail %q, want an interruption", got)
	}
	if n := countCalls(tr, "PressKey"); n != 2 {
		t.Errorf("got %d iterations after cancel, want 2", n)
	}
}

func TestRepeatCommand_NestedLoopsMultiply(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 2\n    commands:\n      - repeat:\n          times: 3\n          commands:\n            - back\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := countCalls(tr, "PressKey"); got != 6 {
		t.Errorf("got %d presses, want 6", got)
	}
}

func TestRepeatCommand_SummaryDescribesTheBounds(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"- repeat:\n    times: 3\n    commands:\n      - back\n", "repeat: 3 times"},
		{"- repeat:\n    times: 1\n    commands:\n      - back\n", "repeat: 1 time"},
		{"- repeat:\n    while:\n      visible: More\n    commands:\n      - back\n", "repeat: while visible element.text = \"More\""},
		{"- repeat:\n    times: 2\n    while:\n      notVisible: Done\n    commands:\n      - back\n", "repeat: 2 times, while notVisible element.text = \"Done\""},
		{"- repeat:\n    while:\n      expr: n < 3\n    commands:\n      - back\n", "repeat: while \"n < 3\""},
	}
	for _, tc := range cases {
		def := mustParse(t, tc.yaml)
		if got := SummarizeStep(def.Steps[0]).Text; got != tc.want {
			t.Errorf("summary = %q, want %q", got, tc.want)
		}
	}
}

func TestRepeatCommand_IsANestingCommand(t *testing.T) {
	if _, ok := CommandDefinition(RepeatCommand{}).(nestingCommand); !ok {
		t.Fatalf("repeat runs nested steps, so a paused run must refuse to run it alone")
	}
}
