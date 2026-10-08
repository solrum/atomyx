package debugrun

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
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
