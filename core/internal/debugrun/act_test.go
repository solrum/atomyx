package debugrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
	"dev.atomyx/internal/session"
)

const tapFlow = "appId: com.test\n---\n" +
	"- screenshot: a\n" + // line 3
	"- tap: Missing\n" // line 4

func pausedAt(t *testing.T, yaml string, line int) *harness {
	t.Helper()
	h := newHarness(t, breakAt(line))
	h.adapter.opts.Finder = h.orch
	h.driver.StageHierarchy(goButtonTree())
	h.start(yaml)
	h.awaitPause()
	return h
}

func finish(h *harness) {
	h.resume(session.ResumeOptions{Force: true})
	h.awaitDone()
}

func errorCode(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func TestActRunsAStepAndLeavesTheRunVariablesAlone(t *testing.T) {
	h := pausedAt(t, tapFlow, 3)
	before, _ := h.adapter.Variables()

	res, err := h.adapter.Act(context.Background(), "read: { selector: { id: go }, field: text, as: scratch }")
	if err != nil || res.Outcome != "passed" || res.Command != "read" {
		t.Fatalf("act = %+v err=%v, want a passed read", res, err)
	}
	after, _ := h.adapter.Variables()
	if _, leaked := after.Variables["scratch"]; leaked || len(after.Variables) != len(before.Variables) {
		t.Fatalf("act wrote into the run: before %v after %v", before.Variables, after.Variables)
	}
	finish(h)
}

func TestActRefusesStepsThatRunOtherSteps(t *testing.T) {
	h := pausedAt(t, tapFlow, 3)
	for _, step := range []string{"runFlow: other.yaml", "- handle:\n    - when:\n        visible: Go\n      do:\n        - screenshot: x"} {
		if _, err := h.adapter.Act(context.Background(), step); errorCode(err) != diag.CodeInvalidArgument {
			t.Errorf("%q: code %q, want %q", step, errorCode(err), diag.CodeInvalidArgument)
		}
	}
	finish(h)
}

func TestActReportsAFailedStepAndRejectsUnreadableText(t *testing.T) {
	h := pausedAt(t, tapFlow, 3)
	res, err := h.adapter.Act(context.Background(), "tap: Nowhere to be found")
	if err != nil || res.Outcome != "failed" || res.Message == "" {
		t.Fatalf("act = %+v err=%v, want a failed result with a message", res, err)
	}
	for _, bad := range []string{"", "tap: [", "- tap: a\n- tap: b"} {
		if _, err := h.adapter.Act(context.Background(), bad); errorCode(err) != diag.CodeInvalidArgument {
			t.Errorf("%q: code %q, want %q", bad, errorCode(err), diag.CodeInvalidArgument)
		}
	}
	finish(h)
}

func TestActAndVariablesRefuseWhenNotPaused(t *testing.T) {
	_, err := New(Options{}).Act(context.Background(), "tap: Go")
	if errorCode(err) != diag.CodeHolderBusy {
		t.Fatalf("code %q, want %q", errorCode(err), diag.CodeHolderBusy)
	}
}

func TestResumeIsAdvisoryWhenTheNextTargetIsMissing(t *testing.T) {
	h := pausedAt(t, tapFlow, 4)

	res := h.resume(session.ResumeOptions{})
	if res.Resumed || res.MissReason == "" {
		t.Fatalf("resume = %+v, want a refusal that says why", res)
	}
	if h.adapter.current() == nil {
		t.Fatal("a refused resume must leave the run paused")
	}
	if forced := h.resume(session.ResumeOptions{Force: true}); !forced.Resumed {
		t.Fatalf("forced resume = %+v, want it to go through", forced)
	}
	h.awaitDone()
}

func TestResumeChecksOnlyImmediateActionsWithATarget(t *testing.T) {
	h := pausedAt(t, tapFlow, 3)
	if res := h.resume(session.ResumeOptions{}); !res.Resumed {
		t.Fatalf("a screenshot step was checked: %+v", res)
	}
	h.awaitDone()
}

// leakyRun is a paused run whose step failure echoes a secret value.
type leakyRun struct{}

func (leakyRun) ExecuteStep(context.Context, script.ScriptStep) (runner.CommandResult, error) {
	detail := "the field held hunter2hunter2 when it should not"
	return runner.CommandResult{Ok: false, Detail: &detail}, nil
}

func (leakyRun) Variables() runner.VariableSnapshot {
	return runner.VariableSnapshot{
		Values: map[string]runner.ScriptValue{"PASSWORD": expression.StringValue("hunter2hunter2")},
		Secret: map[string]bool{"PASSWORD": true},
	}
}

func TestActMessageNeverShowsASecretValue(t *testing.T) {
	a := New(Options{})
	a.paused = &pauseState{run: leakyRun{}}
	res, err := a.Act(context.Background(), "tap: Go")
	if err != nil || res.Outcome != "failed" {
		t.Fatalf("act = %+v err=%v, want a failed result", res, err)
	}
	if strings.Contains(res.Message, "hunter2hunter2") {
		t.Fatalf("message leaks the secret: %q", res.Message)
	}
}

func TestActRecordsTheWrittenStepWithTypedTextMaskedAndFallsBackToTheCommandName(t *testing.T) {
	h := pausedAt(t, tapFlow, 3)
	cases := []struct{ step, want string }{
		{"tap: Go", "- tap: Go"},
		{"type: hunter2hunter2", "- type: '" + orchestra.SecureMask + "'"},
		{"screenshot: shot", "screenshot"},
	}
	for _, c := range cases {
		res, err := h.adapter.Act(context.Background(), c.step)
		if err != nil {
			t.Fatalf("%q: %v", c.step, err)
		}
		if res.Step != c.want || strings.Contains(res.Step, "hunter2hunter2") {
			t.Errorf("%q: Step = %q, want %q", c.step, res.Step, c.want)
		}
	}
	finish(h)
}

// shootingRun is a paused run whose step reports a screenshot file.
type shootingRun struct {
	leakyRun
	adapter *Adapter
}

func (r shootingRun) ExecuteStep(context.Context, script.ScriptStep) (runner.CommandResult, error) {
	r.adapter.NoteArtifact("artifacts/shot.png")
	return runner.CommandResult{Ok: true}, nil
}

func TestActReportsOnlyTheFilesNotedWhileItRuns(t *testing.T) {
	a := New(Options{})
	a.NoteArtifact("artifacts/before.png")
	a.paused = &pauseState{run: shootingRun{adapter: a}}

	res, err := a.Act(context.Background(), "screenshot: shot")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0] != "artifacts/shot.png" {
		t.Fatalf("artifacts = %v, want only the file noted during the act", res.Artifacts)
	}
	a.NoteArtifact("artifacts/after.png")
	if again, _ := a.Act(context.Background(), "screenshot: shot"); len(again.Artifacts) != 1 {
		t.Fatalf("a file noted between acts leaked into the next one: %v", again.Artifacts)
	}
}

// capturingRun records the step it was asked to run.
type capturingRun struct {
	leakyRun
	got script.ScriptStep
}

func (r *capturingRun) ExecuteStep(_ context.Context, step script.ScriptStep) (runner.CommandResult, error) {
	r.got = step
	return runner.CommandResult{Ok: true}, nil
}

func TestActResolvesPlaceholdersAgainstTheRunVariablesWithoutRecordingTheValue(t *testing.T) {
	cases := []struct{ step, wantStep string }{
		{"tap: ${PASSWORD}", "- tap: ${PASSWORD}"},
		{"type: ${PASSWORD}", "- type: '" + orchestra.SecureMask + "'"},
	}
	for _, c := range cases {
		run := &capturingRun{}
		a := New(Options{})
		a.paused = &pauseState{run: run}
		res, err := a.Act(context.Background(), c.step)
		if err != nil || res.Outcome != "passed" {
			t.Fatalf("%q: act = %+v err=%v", c.step, res, err)
		}
		var ran string
		switch s := run.got.(type) {
		case *script.TapStep:
			ran = *s.Selector.Text
		case *script.TypeStep:
			ran = s.Text
		}
		if ran != "hunter2hunter2" {
			t.Errorf("%q: step ran with %q, want the variable value", c.step, ran)
		}
		if res.Step != c.wantStep || strings.Contains(res.Step, "hunter2hunter2") {
			t.Errorf("%q: Step = %q, want %q", c.step, res.Step, c.wantStep)
		}
	}
}

// pinRun is a paused run holding a four digit secret that a failure
// detail echoes.
type pinRun struct{}

func (pinRun) ExecuteStep(context.Context, script.ScriptStep) (runner.CommandResult, error) {
	detail := "the field held 4827 when it should be empty"
	return runner.CommandResult{Ok: false, Detail: &detail}, nil
}

func (pinRun) Variables() runner.VariableSnapshot {
	return runner.VariableSnapshot{
		Values: map[string]runner.ScriptValue{"PIN": expression.StringValue("4827")},
		Secret: map[string]bool{"PIN": true},
	}
}

func TestActWithholdsTheMessageOfAStepThatNamesAShortSecret(t *testing.T) {
	a := New(Options{})
	a.paused = &pauseState{run: pinRun{}}
	for _, step := range []string{"tap: ${PIN}", "type: 4827"} {
		res, err := a.Act(context.Background(), step)
		if err != nil || res.Outcome != "failed" {
			t.Fatalf("%q: act = %+v err=%v", step, res, err)
		}
		if res.Message != orchestra.SecureMask {
			t.Errorf("%q: message = %q, want it withheld", step, res.Message)
		}
	}
	res, _ := a.Act(context.Background(), "tap: Go")
	if strings.Contains(res.Message, "4827") || !strings.Contains(res.Message, "held") {
		t.Errorf("unrelated step: message = %q, want the value masked in place", res.Message)
	}
}

// blockUntilCancelled is a screenshot command that reports entry and
// then waits for its context to end.
type blockUntilCancelled struct {
	entered chan struct{}
	seen    chan error
}

func (blockUntilCancelled) Command() string { return "screenshot" }

func (b blockUntilCancelled) Execute(_ script.ScriptStep, ctx *runner.CommandContext) runner.CommandResult {
	b.entered <- struct{}{}
	<-ctx.Context.Done()
	b.seen <- ctx.Context.Err()
	return runner.CommandResult{Ok: false}
}

func TestRunEndCancelsAnActInFlight(t *testing.T) {
	block := blockUntilCancelled{entered: make(chan struct{}, 1), seen: make(chan error, 1)}
	commands := []runner.CommandDefinition{block}
	h := newHarnessWith(t, breakAt(3), commands)
	h.start(threeShots)
	h.awaitPause()

	actDone := make(chan error, 1)
	go func() {
		_, err := h.adapter.Act(context.Background(), "screenshot: x")
		actDone <- err
	}()
	<-block.entered

	h.cancel()
	out := h.awaitDone()
	if !errors.Is(out.err, context.Canceled) {
		t.Fatalf("run err = %v, want context.Canceled", out.err)
	}
	select {
	case err := <-block.seen:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("act saw %v, want context.Canceled", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("the act never saw a cancellation")
	}
	<-actDone
}

func TestPausedRunTypeStepNeverReportsTheLengthOfASecret(t *testing.T) {
	h := newHarness(t, breakAt(5))
	focused := true
	tree := goButtonTree()
	tree.Children[0].Focused = &focused
	h.driver.StageHierarchy(tree)
	h.startWithEnv("appId: com.test\nenv:\n  LITERAL: x\n---\n- screenshot: a\n- screenshot: b\n", map[string]string{"PASSWORD": "hunter2hunter2"})
	h.awaitPause()
	run := h.adapter.current().run
	typed := func(text string) string {
		step, err := parseActStep("type: "+text, map[string]string{"PASSWORD": "hunter2hunter2"})
		if err != nil {
			t.Fatal(err)
		}
		result, err := run.ExecuteStep(context.Background(), step)
		if err != nil || !result.Ok || result.Detail == nil {
			t.Fatalf("type %q: result = %+v err=%v", text, result, err)
		}
		return *result.Detail
	}

	if got := typed("${PASSWORD}"); got != "Typed a secret value into focused field" {
		t.Errorf("secret detail = %q, want no length", got)
	}
	if got := typed("hello"); got != "Typed 5 characters into focused field" {
		t.Errorf("plain detail = %q, want the count", got)
	}
	finish(h)
}
