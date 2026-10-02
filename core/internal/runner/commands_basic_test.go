package runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

func rootTree(children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}, Children: children}
}

func button(id, text, bounds string) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: map[string]string{"id": id, "text": text, "label": text, "bounds": bounds}}
}

func focusedButton(id, text, bounds string) wire.TreeNodeWire {
	n := button(id, text, bounds)
	focused := true
	n.Focused = &focused
	return n
}

func TestLaunchAppCommand(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- launchApp\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Launched com.test" {
		t.Fatalf("got detail %q", got)
	}
	if tr.driver.RunningApp == nil || *tr.driver.RunningApp != "com.test" {
		t.Fatalf("got RunningApp=%v, want com.test", tr.driver.RunningApp)
	}
}

func TestLaunchAppCommand_NoAppID(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "name: no appId\n---\n- launchApp\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if got := detail(lastStep(t, result)); got != "No appId specified in script config" {
		t.Fatalf("got detail %q", got)
	}
}

// TestLaunchAppCommand_NotInstalled is the regression for a launchApp
// step whose appId names an app absent from the device: the step
// fails with the orchestra's own not-installed message, the run
// reports an ordinary failure, and the failure never reaches the
// runner's panic-recovery path (no "command handler panicked" log
// entry), because the not-installed condition is an expected outcome
// of running the script against the wrong target, not a bug.
func TestLaunchAppCommand_NotInstalled(t *testing.T) {
	tr := newTestRunner()
	tr.driver.Apps = nil
	tr.driver.StageHierarchy(rootTree())
	logger := newCapturingLogger()
	tr.runner.deps.Logger = logger
	tr.runner.stepLog = logger

	result := run(t, tr, "appId: com.test\n---\n- launchApp\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := `app "com.test" is not installed on the target device`
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q (no \"Exception:\" prefix)", got, want)
	}
	for _, e := range *logger.errors {
		if e.msg == "command handler panicked" {
			t.Fatalf("logged a panic-recovery entry for an expected not-installed failure: %+v", e)
		}
	}
}

// TestLaunchAppCommand_DedupedLaunchOmittedFromReport proves a second
// launchApp for an app already launched earlier in the same flow
// produces no entry in the report's steps array at all: the step
// count drops by exactly one and the surviving steps are unchanged.
func TestLaunchAppCommand_DedupedLaunchOmittedFromReport(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n"+
		"- launchApp\n"+
		"- screenshot: mid\n"+
		"- launchApp\n"+
		"- screenshot: after\n")

	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if len(result.Steps) != 3 {
		t.Fatalf("got %d steps, want 3 (the second launchApp omitted): %s", len(result.Steps), debugSteps(result.Steps))
	}
	launches := 0
	for _, s := range result.Steps {
		if s.Command == "launchApp" {
			launches++
		}
	}
	if launches != 1 {
		t.Fatalf("got %d launchApp entries in the report, want exactly 1: %s", launches, debugSteps(result.Steps))
	}
	if result.Steps[1].Command != "screenshot" {
		t.Fatalf("got second surviving step %+v, want the \"mid\" screenshot untouched", result.Steps[1])
	}
	if result.Steps[2].Command != "screenshot" {
		t.Fatalf("got third surviving step %+v, want the \"after\" screenshot untouched", result.Steps[2])
	}
}

// TestLaunchAppCommand_DedupedLaunchEmitsNoStepEventsOrTrace proves the
// omission is complete: a deduped launchApp step fires neither a
// StepStarted nor a StepCompleted event, and leaves no "script.step"
// trace log line, as opposed to executing quietly and only hiding
// from the report.
func TestLaunchAppCommand_DedupedLaunchEmitsNoStepEventsOrTrace(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(rootTree())
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	logger := newRecordingLogger()

	var events []StepEvent
	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra: orch,
		Clock:     clock,
		Logger:    logger,
		Commands:  DefaultCommands(),
		OnStep:    func(e StepEvent) { events = append(events, e) },
	})

	def := mustParse(t, "appId: com.test\n---\n- launchApp\n- launchApp\n")
	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}

	launchEvents := 0
	for _, e := range events {
		if e.Command == "launchApp" {
			launchEvents++
		}
	}
	if launchEvents != 2 {
		t.Fatalf("got %d launchApp step events (started+completed), want exactly 2 (one pair for the real launch, none for the deduped one)", launchEvents)
	}

	launchLogs := 0
	for _, l := range *logger.logs {
		if l.ctx["command"] == "launchApp" {
			launchLogs++
		}
	}
	if launchLogs != 1 {
		t.Fatalf("got %d \"script.step\" trace lines for launchApp, want exactly 1", launchLogs)
	}
}

// TestLaunchAppCommand_FreshLaunchNotSkipped proves an explicit fresh
// launch (noReset false) is never treated as a dedup no-op, even when
// the app was already launched earlier in the flow: it still executes
// and still produces a report entry.
func TestLaunchAppCommand_FreshLaunchNotSkipped(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(rootTree())
	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
	fresh := false

	r := NewScriptRunner(ScriptRunnerDeps{
		Orchestra:     orch,
		Clock:         clock,
		Logger:        noopLogger{},
		Commands:      DefaultCommands(),
		LaunchNoReset: &fresh,
	})

	def := mustParse(t, "appId: com.test\n---\n- launchApp\n- launchApp\n")
	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if len(result.Steps) != 2 {
		t.Fatalf("got %d steps, want 2 (a fresh launch must never be skipped): %s", len(result.Steps), debugSteps(result.Steps))
	}
}

// TestLaunchAppCommand_NestedDedupedLaunchOmitted proves the same
// omission holds for a launchApp step reached through a nested step
// list (here, a runFlow sub-flow), not only for the top-level loop.
func TestLaunchAppCommand_NestedDedupedLaunchOmitted(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	absPath, _ := filepath.Abs("flows/relaunch.yml")
	tr.loader.stage(absPath, "name: relaunch\n---\n- launchApp\n- screenshot: after\n")

	var events []StepEvent
	tr.runner.deps.OnStep = func(e StepEvent) { events = append(events, e) }

	yaml := "appId: com.test\n---\n- launchApp\n- runFlow:\n    file: flows/relaunch.yml\n"
	result := run(t, tr, yaml)

	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != `Sub-flow "relaunch" completed (2 steps)` {
		t.Fatalf("got detail %q", got)
	}

	launchEvents := 0
	for _, e := range events {
		if e.Command == "launchApp" {
			launchEvents++
		}
	}
	if launchEvents != 2 {
		t.Fatalf("got %d launchApp step events (started+completed), want exactly 2 for the top-level launch only, none for the nested deduped one", launchEvents)
	}
}

func TestTapCommand(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("btn1", "Login", "0,200,100,250")))

	result := run(t, tr, "appId: com.test\n---\n- tap: Login\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	found := false
	for _, c := range tr.driver.Calls {
		if c.Method == "Tap" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Tap was not dispatched: %+v", tr.driver.Calls)
	}
}

func TestTapCommand_NotFound(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- tap: Missing\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
}

func TestTypeCommand_IntoField(t *testing.T) {
	tr := newTestRunner()
	// The field reports focus so the input-focus gate resolves and the
	// text is dispatched into it.
	tr.driver.StageHierarchy(rootTree(focusedButton("email", "", "0,200,100,250")))

	result := run(t, tr, "appId: com.test\n---\n- type:\n    into:\n      id: email\n    text: hello\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Typed 5 characters" {
		t.Fatalf("got detail %q", got)
	}
}

func TestTypeCommand_BareIntoFocused(t *testing.T) {
	tr := newTestRunner()
	// A focused field makes the input-ready gate resolve so the bare
	// type dispatches into whatever the author focused.
	tr.driver.StageHierarchy(rootTree(focusedButton("email", "", "0,200,100,250")))

	result := run(t, tr, "appId: com.test\n---\n- type: hello\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Typed 5 characters into focused field" {
		t.Fatalf("got detail %q", got)
	}
}

// TestTypeCommand_IntoField_FocusNeverSettles is the regression for a
// type step whose target field never takes input focus because a
// different field still holds it: the step fails with the orchestra's
// own focus-timeout message instead of the runner's panic-recovery
// path, because a field that never focuses is an expected outcome of
// the screen's current state, not a bug in the handler.
func TestTypeCommand_IntoField_FocusNeverSettles(t *testing.T) {
	tr := newTestRunner()
	// email holds focus; the password target sits below it, disjoint,
	// so the focus gate never matches the target.
	tr.driver.StageHierarchy(rootTree(
		focusedButton("email", "", "0,100,300,150"),
		button("password", "", "0,200,300,250"),
	))
	logger := newCapturingLogger()
	tr.runner.deps.Logger = logger
	tr.runner.stepLog = logger

	result := run(t, tr, "appId: com.test\n---\n- type:\n    into:\n      id: password\n    text: secret\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if got := detail(lastStep(t, result)); strings.HasPrefix(got, "Exception:") {
		t.Fatalf("got detail %q, want no \"Exception:\" prefix", got)
	}
	for _, e := range *logger.errors {
		if e.msg == "command handler panicked" {
			t.Fatalf("logged a panic-recovery entry for an expected focus-timeout failure: %+v", e)
		}
	}
}

func TestWaitForCommand_TimesOut(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- waitFor:\n    text: Missing\n    timeout: 200\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if got := detail(lastStep(t, result)); got != "Element not found within 200ms" {
		t.Fatalf("got detail %q", got)
	}
}

func TestAssertVisibleCommand_InstantCheck(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("btn", "Ready", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\n---\n- assertVisible: Ready\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
}

func TestAssertNotVisibleCommand_InstantCheck(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- assertNotVisible: Gone\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
}

func TestAssertNotVisibleCommand_PollsUntilGone(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("btn", "Loading", "0,0,10,10")))
	tr.driver.StageHierarchy(rootTree(button("btn", "Loading", "0,0,10,10")))
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- assertNotVisible:\n    text: Loading\n    timeout: 1000\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
}

func TestAssertExistsCommand_InstantCheck(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("btn", "Ready", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\n---\n- assertExists: Ready\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Element exists" {
		t.Fatalf("got detail %q", got)
	}
}

func TestAssertExistsCommand_InstantCheck_Absent(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- assertExists: Missing\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if got := detail(lastStep(t, result)); got != "Element not found" {
		t.Fatalf("got detail %q", got)
	}
}

func TestAssertExistsCommand_PollsUntilPresent(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.driver.StageHierarchy(rootTree())
	tr.driver.StageHierarchy(rootTree(button("btn", "Ready", "0,0,10,10")))

	result := run(t, tr, "appId: com.test\n---\n- assertExists:\n    text: Ready\n    timeout: 1000\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Element exists" {
		t.Fatalf("got detail %q", got)
	}
}

func TestAssertExistsCommand_TimesOut(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- assertExists:\n    text: Missing\n    timeout: 200\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if got := detail(lastStep(t, result)); got != "Element not found within 200ms" {
		t.Fatalf("got detail %q", got)
	}
}

func TestScreenshotCommand_DefaultLabel(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- screenshot\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 1 || shots[0].Label != "step-1" {
		t.Fatalf("got screenshots %+v, want [step-1]", shots)
	}
}

func TestSwipeCommand(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- swipe: up\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Swiped up" {
		t.Fatalf("got detail %q", got)
	}
}

func TestPressKeyCommand(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- pressKey: enter\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Pressed enter" {
		t.Fatalf("got detail %q", got)
	}
}

func TestBackCommand(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- back\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Pressed back" {
		t.Fatalf("got detail %q", got)
	}
}

func TestSleepCommand_AdvancesFakeClock(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	before := tr.clock.NowMs()

	result := run(t, tr, "appId: com.test\n---\n- sleep: 500\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "Slept 500ms" {
		t.Fatalf("got detail %q", got)
	}
	if after := tr.clock.NowMs(); after-before != 500 {
		t.Fatalf("got clock advance %dms, want 500ms", after-before)
	}
}

// TestSleepCommand_InterruptedByCancellationIsNotLoggedAsAPanic proves a
// run interrupted during a sleep step ends that step as interrupted and
// writes no "command handler panicked" entry, because the interruption
// is the operator's own stop and not a handler bug.
func TestSleepCommand_InterruptedByCancellationIsNotLoggedAsAPanic(t *testing.T) {
	tr := newTestRunner()
	logger := newCapturingLogger()
	tr.runner.deps.Logger = logger
	tr.runner.stepLog = logger
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result := tr.runner.execute(SleepCommand{}, &script.SleepStep{Command: script.CommandSleep, Ms: 1000}, &CommandContext{
		Context: runCtx, Orchestra: tr.orch, Clock: tr.clock, Logger: logger,
	})

	if result.Ok || result.Detail == nil || !strings.HasPrefix(*result.Detail, "Interrupted: ") {
		t.Fatalf("result = %+v, want a failed step whose detail starts with Interrupted", result)
	}
	for _, e := range append(*logger.errors, *logger.debugs...) {
		if e.msg == "command handler panicked" {
			t.Fatalf("logged a panic entry for a canceled sleep: %+v", e)
		}
	}
}

// TestExecute_ACancellationErrorFromALiveContextStillCountsAsAPanic keeps
// the recovery honest: a handler that panics with a context error while
// the run's context is still live has a bug, and is still logged.
func TestExecute_ACancellationErrorFromALiveContextStillCountsAsAPanic(t *testing.T) {
	tr := newTestRunner(cancelPanicCommand{})
	logger := newCapturingLogger()
	tr.runner.deps.Logger = logger
	tr.runner.stepLog = logger

	result := run(t, tr, "appId: com.test\n---\n- back\n")

	if result.Ok || !strings.HasPrefix(detail(lastStep(t, result)), "Exception: ") {
		t.Fatalf("result = %s, want an Exception step", debugSteps(result.Steps))
	}
	logged := false
	for _, e := range *logger.errors {
		logged = logged || e.msg == "command handler panicked"
	}
	if !logged {
		t.Fatal("no panic entry was logged for a handler bug")
	}
}

type cancelPanicCommand struct{}

func (cancelPanicCommand) Command() string { return script.CommandBack }
func (cancelPanicCommand) Execute(script.ScriptStep, *CommandContext) CommandResult {
	panic(context.Canceled)
}

func TestTypedDetail_CountsRunesAndNeverEchoesText(t *testing.T) {
	const secret = "pässwörd🙂"
	got := typedDetail(secret, "")
	if got != "Typed 9 characters" {
		t.Fatalf("got %q, want the rune count", got)
	}
	if strings.Contains(got, "ss") {
		t.Fatalf("detail %q echoes the typed text", got)
	}
}

func TestSummarizeStep_TypeNeverShowsTheTypedText(t *testing.T) {
	def := mustParse(t, "appId: com.test\n---\n- type:\n    into:\n      id: nickname\n    text: plain-sentinel\n")

	summary := SummarizeStep(def.Steps[0])

	if strings.Contains(summary.Text, "plain-sentinel") {
		t.Fatalf("summary %q carries the typed text", summary.Text)
	}
	for _, token := range summary.Tokens {
		if strings.Contains(token.Text, "plain-sentinel") {
			t.Fatalf("token %+v carries the typed text", token)
		}
	}
}
