package inspector

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
)

func TestHostOverAViewer_StepLinesReachTheEventsFeed(t *testing.T) {
	v := newFakeViewer()
	detail := "tapped"
	v.lines = []runfolder.StepLine{
		{RunID: "run-1", Seq: 1, StepIndex: 0, Command: "tap", Phase: runfolder.StepPhaseCompleted, Ok: true, DurationMs: 12, Detail: &detail},
		{RunID: "run-1", Seq: 2, StepIndex: 1, Command: "assert", Phase: runfolder.StepPhaseCompleted, Ok: false, DurationMs: 5},
	}
	v.feed <- session.Observation{Kind: session.ObservedStep, Revision: 2, Stable: true}
	close(v.feed)
	got := readStepEvents(t, v)
	if len(got) != 2 {
		t.Fatalf("events = %+v, want two step lines", got)
	}
	first, second := got[0], got[1]
	if first.Kind != "step" || first.RunID != "run-1" || first.Seq != 1 || first.Command != "tap" || first.Ok == nil || !*first.Ok || first.Detail != "tapped" {
		t.Errorf("first = %+v", first)
	}
	if second.Seq != 2 || second.Command != "assert" || second.Ok == nil || *second.Ok || second.Detail != "" {
		t.Errorf("second = %+v", second)
	}
}

// eventView decodes any line of the feed, a step line or a state line.
type eventView struct {
	stepEventLine
	State string                 `json:"state"`
	Pause *session.PauseLocation `json:"pause"`
}

// readStepEvents serves the events feed of v until its feed closes and
// returns every line, decoded as an eventView, so a line of another kind
// shows through its Kind.
func readStepEvents(t *testing.T, v *fakeViewer) []eventView {
	t.Helper()
	return readStepEventsAt(t, v, routeEvents)
}

func readStepEventsAt(t *testing.T, v *fakeViewer, target string) []eventView {
	t.Helper()
	srv, err := New(Config{Source: v, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: []RouteGroup{ReadRoutes}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	req.Host = srv.hostPort()
	req.Header.Set(tokenHeader, authorize(t, srv))

	srv.Handler().ServeHTTP(rec, req)

	var got []eventView
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		var l eventView
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		got = append(got, l)
	}
	return got
}

func TestHostOverAViewer_ALostStretchOfStepLinesIsReportedOnce(t *testing.T) {
	v := newFakeViewer()
	v.gap = true
	v.lines = []runfolder.StepLine{{RunID: "run-1", Seq: 5, Command: "tap", Phase: runfolder.StepPhaseCompleted, Ok: true}}
	v.feed <- session.Observation{Kind: session.ObservedStepGap, Revision: 5, Stable: true}
	v.feed <- session.Observation{Kind: session.ObservedStep, Revision: 5, Stable: true}
	close(v.feed)

	got := readStepEvents(t, v)

	var kinds []string
	for _, l := range got {
		kinds = append(kinds, l.Kind)
	}
	if strings.Join(kinds, ",") != "step_gap,step" {
		t.Fatalf("event kinds = %v, want one step_gap then the step", kinds)
	}
}

func TestHostOverAViewer_AStartedLineCarriesNoOutcome(t *testing.T) {
	v := newFakeViewer()
	v.lines = []runfolder.StepLine{
		{RunID: "run-1", Seq: 1, StepIndex: 0, Command: "tap", Phase: runfolder.StepPhaseStarted},
		{RunID: "run-1", Seq: 2, StepIndex: 0, Command: "tap", Phase: runfolder.StepPhaseCompleted, Ok: false},
	}
	v.feed <- session.Observation{Kind: session.ObservedStep, Revision: 2, Stable: true}
	close(v.feed)

	got := readStepEvents(t, v)

	if len(got) != 2 {
		t.Fatalf("events = %+v, want two", got)
	}
	if got[0].Ok != nil {
		t.Errorf("started line carries ok = %v, want none", *got[0].Ok)
	}
	if got[1].Ok == nil || *got[1].Ok {
		t.Errorf("completed failing line ok = %v, want a present false", got[1].Ok)
	}
}

func TestHostOverAViewer_AHolderStateTransitionIsRelayedAsAStateEvent(t *testing.T) {
	v := newFakeViewer()
	v.feed <- session.Observation{Kind: session.ObservedState, State: session.StateRunning, Command: "tap", RunID: "run-1", Stable: true}
	close(v.feed)

	got := readStateEvents(t, v)

	want := stateEventLine{Kind: "state", State: "running", Command: "tap", RunID: "run-1"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("events = %+v, want [%+v]", got, want)
	}
}

func TestHostOverAViewer_APausedStateEventCarriesTheLocation(t *testing.T) {
	v := newFakeViewer()
	loc := &session.PauseLocation{Source: "/work/login.yaml", Line: 12, Depth: 1, Command: "tap"}
	v.feed <- session.Observation{Kind: session.ObservedState, State: session.StatePaused, Pause: loc, Stable: true}
	close(v.feed)

	got := readStepEvents(t, v)

	if len(got) != 1 || got[0].State != "paused" || got[0].Pause == nil || *got[0].Pause != *loc {
		t.Fatalf("events = %+v, want one paused event at %+v", got, *loc)
	}
}

func readStateEvents(t *testing.T, v *fakeViewer) []stateEventLine {
	t.Helper()
	var out []stateEventLine
	for _, l := range readStepEvents(t, v) {
		out = append(out, stateEventLine{Kind: l.Kind, State: l.State, Command: l.Command, RunID: l.RunID})
	}
	return out
}

func TestHostOverAViewer_AReconnectingClientResumesAfterItsLastStepLine(t *testing.T) {
	v := newFakeViewer()
	close(v.feed)

	readStepEventsAt(t, v, routeEvents+"?runId=run-1&seq=7")

	if len(v.resumes) != 1 || v.resumes[0] != (resume{runID: "run-1", seq: 7}) {
		t.Fatalf("subscribe calls = %+v, want one resume after run-1 line 7", v.resumes)
	}
}

func TestHostOverAViewer_ARefusedResumeFallsBackToTheFirstHeldLine(t *testing.T) {
	v := newFakeViewer()
	v.rejectResume = true
	close(v.feed)

	readStepEventsAt(t, v, routeEvents+"?runId=run-1&seq=99")

	if len(v.resumes) != 2 || v.resumes[1] != (resume{}) {
		t.Fatalf("subscribe calls = %+v, want the refused resume then one from the start", v.resumes)
	}
}
