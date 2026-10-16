package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/script"
)

const retryYAML = "appId: com.test\n---\n- retry:\n    maxRetries: %s\n    commands:\n      - assertVisible: Ready\n      - back\n"

func retryScript(maxRetries string) string {
	return strings.Replace(retryYAML, "%s", maxRetries, 1)
}

func TestRetryCommand_PassesOnTheFirstAttemptWithoutRerunning(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("ok", "Ready", "0,0,10,10")))

	result := run(t, tr, retryScript("2"))
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "retry: passed on attempt 1 of 3" {
		t.Errorf("got detail %q", got)
	}
	if n := countCalls(tr, "PressKey"); n != 1 {
		t.Errorf("got %d back presses, want 1", n)
	}
}

func TestRetryCommand_ReRunsTheWholeBlockUntilItPasses(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchyRepeated(rootTree(), 2)
	tr.driver.StageHierarchy(rootTree(button("ok", "Ready", "0,0,10,10")))

	var events []StepEvent
	tr.runner.deps.OnStep = func(e StepEvent) { events = append(events, e) }

	result := run(t, tr, retryScript("3"))
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "retry: passed on attempt 3 of 4" {
		t.Errorf("got detail %q", got)
	}

	failedAsserts, passedAsserts := 0, 0
	for _, e := range events {
		if e.Type == StepCompleted && e.Depth == 1 && e.Command == script.CommandAssertVisible {
			if e.Ok {
				passedAsserts++
			} else {
				failedAsserts++
			}
		}
	}
	if failedAsserts != 2 || passedAsserts != 1 {
		t.Errorf("got %d failed and %d passed attempts in the events, want 2 and 1", failedAsserts, passedAsserts)
	}
}

func TestRetryCommand_FinalFailureCarriesTheLastAttemptsError(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, retryScript("2"))
	if result.Ok {
		t.Fatalf("a block that never passes must fail")
	}
	got := detail(lastStep(t, result))
	if !strings.HasPrefix(got, "retry: attempt 3 of 3 failed: ") {
		t.Errorf("got detail %q, want it to name attempt 3 of 3", got)
	}
	if !strings.Contains(got, "Element not visible") {
		t.Errorf("got detail %q, want it to carry the nested failure", got)
	}
	if n := countCalls(tr, "PressKey"); n != 0 {
		t.Errorf("got %d back presses, want 0 because the assertion always fails first", n)
	}
}

func TestRetryCommand_ZeroRetriesRunsOnce(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	var started int
	tr.runner.deps.OnStep = func(e StepEvent) {
		if e.Type == StepStarted && e.Depth == 1 {
			started++
		}
	}

	result := run(t, tr, retryScript("0"))
	if result.Ok {
		t.Fatalf("a failing block with no retries must fail")
	}
	if started != 1 {
		t.Errorf("got %d nested steps started, want 1", started)
	}
	if got := detail(lastStep(t, result)); !strings.HasPrefix(got, "retry: attempt 1 of 1 failed: ") {
		t.Errorf("got detail %q", got)
	}
}

func TestRetryCommand_DefaultsToOneRetry(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	var started int
	tr.runner.deps.OnStep = func(e StepEvent) {
		if e.Type == StepStarted && e.Depth == 1 {
			started++
		}
	}

	result := run(t, tr, "appId: com.test\n---\n- retry:\n    commands:\n      - assertVisible: Ready\n")
	if result.Ok {
		t.Fatalf("a failing block must fail")
	}
	if started != script.DefaultRetryMaxRetries+1 {
		t.Errorf("got %d attempts, want %d", started, script.DefaultRetryMaxRetries+1)
	}
	if got := detail(lastStep(t, result)); !strings.HasPrefix(got, "retry: attempt 2 of 2 failed: ") {
		t.Errorf("got detail %q", got)
	}
}

func TestRetryCommand_MaximumRetriesRunsFourAttempts(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	var started int
	tr.runner.deps.OnStep = func(e StepEvent) {
		if e.Type == StepStarted && e.Depth == 1 {
			started++
		}
	}

	run(t, tr, retryScript("3"))
	if started != 4 {
		t.Errorf("got %d attempts, want 4", started)
	}
}

func TestRetryCommand_StopsWhenTheRunContextIsCanceled(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var started int
	tr.runner.deps.OnStep = func(e StepEvent) {
		if e.Type == StepStarted && e.Depth == 1 {
			started++
		}
		if e.Type == StepCompleted && e.Depth == 1 {
			cancel()
		}
	}

	result, err := tr.runner.Run(ctx, mustParse(t, retryScript("3")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatalf("a canceled run must not report success")
	}
	if started != 1 {
		t.Errorf("got %d attempts after cancel, want 1", started)
	}
	if got := detail(lastStep(t, result)); !strings.HasPrefix(got, "Interrupted:") {
		t.Errorf("got detail %q, want an interruption", got)
	}
}

func TestRetryCommand_DoesNotRetryAfterAGateError(t *testing.T) {
	gate := &funcGate{fn: func(s GateStep, _ PausedRun) error {
		if s.Depth == 1 {
			return context.Canceled
		}
		return nil
	}}
	tr := gatedRunner(gate, "")
	tr.driver.StageHierarchy(rootTree())

	_, err := tr.runner.Run(context.Background(), mustParse(t, retryScript("3")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err %v, want context.Canceled", err)
	}
	nested := 0
	for _, s := range gate.seen {
		if s.Depth == 1 {
			nested++
		}
	}
	if nested != 1 {
		t.Errorf("the gate saw %d nested steps, want 1 because the run ended at the first", nested)
	}
}

func TestRetryCommand_SummaryStatesTheRetryBudget(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"- retry:\n    maxRetries: 3\n    commands:\n      - back\n", "retry: up to 3 retries"},
		{"- retry:\n    maxRetries: 1\n    commands:\n      - back\n", "retry: up to 1 retry"},
		{"- retry:\n    maxRetries: 0\n    commands:\n      - back\n", "retry: up to 0 retries"},
		{"- retry:\n    commands:\n      - back\n", "retry: up to 1 retry"},
	}
	for _, tc := range cases {
		def := mustParse(t, tc.yaml)
		if got := SummarizeStep(def.Steps[0]).Text; got != tc.want {
			t.Errorf("summary = %q, want %q", got, tc.want)
		}
	}
}

func TestRetryCommand_IsANestingCommand(t *testing.T) {
	if _, ok := CommandDefinition(RetryCommand{}).(nestingCommand); !ok {
		t.Fatalf("retry runs nested steps, so a paused run must refuse to run it alone")
	}
}

func TestRetryCommand_InsideRepeatNamesBothLevels(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- repeat:\n    times: 2\n    commands:\n      - retry:\n          maxRetries: 1\n          commands:\n            - assertVisible: Ready\n")
	if result.Ok {
		t.Fatalf("a block that never passes must fail")
	}
	got := detail(lastStep(t, result))
	if !strings.HasPrefix(got, "repeat: iteration 1 of 2 failed: retry: attempt 2 of 2 failed: ") {
		t.Errorf("got detail %q", got)
	}
}
