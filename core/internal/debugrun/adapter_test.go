package debugrun

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/session"
)

const threeShots = "appId: com.test\n---\n" +
	"- screenshot: a\n" + // line 3
	"- screenshot: b\n" + // line 4
	"- screenshot: c\n" // line 5

func breakAt(lines ...int) Options {
	var out []runner.BreakLocation
	for _, l := range lines {
		out = append(out, runner.BreakLocation{Source: sourcePath, Line: l})
	}
	return Options{Breakpoints: out}
}

func TestBreakpointPausesBeforeTheStepAndResumeFinishesTheRun(t *testing.T) {
	h := newHarness(t, breakAt(4))
	h.start(threeShots)

	got := h.awaitPause()
	want := session.PauseLocation{Source: sourcePath, Line: 4, Depth: 0, Command: "screenshot"}
	if got != want {
		t.Fatalf("paused at %+v, want %+v", got, want)
	}
	if n := h.started.Load(); n != 1 {
		t.Fatalf("%d steps started before the pause, want 1", n)
	}
	if res := h.resume(session.ResumeOptions{}); !res.Resumed {
		t.Fatalf("resume refused: %+v", res)
	}
	out := h.awaitDone()
	if out.err != nil || !out.result.Ok || h.started.Load() != 3 {
		t.Fatalf("got err=%v ok=%v started=%d, want a finished run of 3 steps", out.err, out.result.Ok, h.started.Load())
	}
	if got := h.adapter.Unhit(); len(got) != 0 {
		t.Fatalf("a breakpoint that stopped the run is reported unhit: %v", got)
	}
}

func TestStepStopsAfterTheNextStep(t *testing.T) {
	h := newHarness(t, breakAt(3))
	h.start(threeShots)

	h.awaitPause()
	h.resume(session.ResumeOptions{Step: true})
	next := h.awaitPause()
	if next.Line != 4 {
		t.Fatalf("stopped at line %d, want 4", next.Line)
	}
	h.resume(session.ResumeOptions{})
	if out := h.awaitDone(); out.err != nil {
		t.Fatal(out.err)
	}
}

func TestUnhitBreakpointIsReportedAtRunEnd(t *testing.T) {
	h := newHarness(t, breakAt(99))
	h.start(threeShots)

	if out := h.awaitDone(); out.err != nil || !out.result.Ok {
		t.Fatalf("run failed: %v", out.err)
	}
	if got := h.adapter.Unhit(); len(got) != 1 || got[0] != loc(99) {
		t.Fatalf("unhit = %v, want [%s]", got, loc(99))
	}
}

func TestResumeRefusesWhenNotPaused(t *testing.T) {
	_, err := New(Options{}).Resume(context.Background(), session.ResumeOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeHolderBusy {
		t.Fatalf("Resume code = %q, want %q", code, diag.CodeHolderBusy)
	}
}

const nestedFlow = "appId: com.test\n---\n" +
	"- launchApp\n" + // line 3
	"- launchApp\n" + // line 4, deduplicated
	"- handle:\n" + // line 5
	"    - when:\n" +
	"        visible: Go\n" +
	"      do:\n" +
	"        - screenshot: n1\n" +
	"- screenshot: last\n"

func TestStepStopsAtANestedStep(t *testing.T) {
	h := newHarness(t, breakAt(5))
	h.driver.StageHierarchy(goButtonTree())
	h.start(nestedFlow)

	h.awaitPause()
	h.resume(session.ResumeOptions{Step: true})
	nested := h.awaitPause()
	want := session.PauseLocation{Source: sourcePath, Line: 0, Depth: 1, Command: "screenshot"}
	if nested != want {
		t.Fatalf("paused at %+v, want %+v", nested, want)
	}
	h.resume(session.ResumeOptions{})
	if out := h.awaitDone(); out.err != nil || !out.result.Ok {
		t.Fatalf("run failed: err=%v", out.err)
	}
}

func TestBreakpointOnADeduplicatedLaunchIsReportedUnhit(t *testing.T) {
	h := newHarness(t, breakAt(4))
	h.driver.StageHierarchy(goButtonTree())
	h.start(nestedFlow)

	if out := h.awaitDone(); out.err != nil || !out.result.Ok {
		t.Fatalf("run failed: %v", out.err)
	}
	if got := h.adapter.Unhit(); len(got) != 1 || got[0] != loc(4) {
		t.Fatalf("unhit = %v, want [%s]", got, loc(4))
	}
}

func TestClosedSessionEndsAPausedRun(t *testing.T) {
	h := newHarness(t, breakAt(3))
	h.start(threeShots)
	h.awaitPause()

	close(h.pauser.closed)
	out := h.awaitDone()
	if !errors.Is(out.err, errSessionClosed) {
		t.Fatalf("run ended with %v, want the session-closed error", out.err)
	}
	if h.pauser.left.Load() == 0 {
		t.Fatal("the pause mark was never cleared")
	}
}

func TestLostDeviceAndCancelledContextEndAPausedRun(t *testing.T) {
	lost := newHarness(t, breakAt(3))
	lost.start(threeShots)
	lost.awaitPause()
	close(lost.pauser.lost)
	if out := lost.awaitDone(); !errors.Is(out.err, errDeviceLost) {
		t.Fatalf("lost: run ended with %v", out.err)
	}

	cancelled := newHarness(t, breakAt(3))
	cancelled.start(threeShots)
	cancelled.awaitPause()
	cancelled.cancel()
	if out := cancelled.awaitDone(); !errors.Is(out.err, context.Canceled) {
		t.Fatalf("cancelled: run ended with %v", out.err)
	}
}

func TestSetBreakpointsReplacesAllAndValidatesBeforeChangingAnything(t *testing.T) {
	opts := breakAt(3)
	opts.Scripts = func() []runner.BreakScript { return threeShotScripts(t) }
	h := newHarness(t, opts)

	if err := h.adapter.SetBreakpoints([]string{sourcePath + ":99"}); err == nil {
		t.Fatal("a line with no step was accepted")
	}
	if got := h.adapter.Unhit(); len(got) != 1 || got[0] != loc(3) {
		t.Fatalf("a refused set changed the breakpoints: %v", got)
	}
	if err := h.adapter.SetBreakpoints([]string{sourcePath + ":5"}); err != nil {
		t.Fatal(err)
	}
	h.start(threeShots)
	if got := h.awaitPause(); got.Line != 5 {
		t.Fatalf("paused at line %d, want 5 (the old breakpoint must be gone)", got.Line)
	}
	h.resume(session.ResumeOptions{})
	h.awaitDone()
}

func TestVariablesAreRedactedAndRefusedWhenNotPaused(t *testing.T) {
	if _, err := New(Options{}).Variables(); err == nil {
		t.Fatal("Variables succeeded while not paused")
	}
	h := newHarness(t, breakAt(5))
	h.startWithEnv("appId: com.test\nenv:\n  LITERAL: written-in-script\n---\n- screenshot: a\n- screenshot: b\n",
		map[string]string{"PASSWORD": "hunter2hunter2"})
	h.awaitPause()
	view, err := h.adapter.Variables()
	if err != nil {
		t.Fatal(err)
	}
	if view.Variables["PASSWORD"] != orchestra.SecureMask {
		t.Fatalf("variables = %v, want PASSWORD masked", view.Variables)
	}
	if view.Variables["LITERAL"] != "written-in-script" {
		t.Fatalf("variables = %v, want the script literal shown", view.Variables)
	}
	h.resume(session.ResumeOptions{})
	h.awaitDone()
}

func TestNestedPauseReportsTheScriptFileBeforeAnyTopLevelStep(t *testing.T) {
	h := newHarness(t, Options{})
	if err := h.adapter.Pause(); err != nil {
		t.Fatal(err)
	}
	h.start("appId: com.test\npreconditions:\n  - check: { visible: \"Home\" }\n    setup:\n      - sleep: 0\n    policy: always\n---\n- launchApp\n")

	got := h.awaitPause()
	want := session.PauseLocation{Source: sourcePath, Line: 0, Depth: 1, Command: "sleep"}
	if got != want {
		t.Fatalf("paused at %+v, want %+v", got, want)
	}
	h.resume(session.ResumeOptions{Force: true})
	h.awaitDone()
}

func TestPauseWhileAlreadyPausedDoesNotStopTheRunAgain(t *testing.T) {
	h := newHarness(t, breakAt(3))
	h.start(threeShots)
	h.awaitPause()

	if err := h.adapter.Pause(); err != nil {
		t.Fatal(err)
	}
	h.resume(session.ResumeOptions{})
	out := h.awaitDone()
	if out.err != nil || !out.result.Ok || h.started.Load() != 3 {
		t.Fatalf("got err=%v ok=%v started=%d, want the run to finish without a second stop", out.err, out.result.Ok, h.started.Load())
	}
}
