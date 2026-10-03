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
		{RunID: "run-1", Seq: 1, StepIndex: 0, Command: "tap", Phase: runfolder.StepPhase("end"), Ok: true, DurationMs: 12, Detail: &detail},
		{RunID: "run-1", Seq: 2, StepIndex: 1, Command: "assert", Phase: runfolder.StepPhase("end"), Ok: false, DurationMs: 5},
	}
	v.feed <- session.Observation{Kind: session.ObservedStep, Revision: 2, Stable: true}
	close(v.feed)
	got := readStepEvents(t, v)
	if len(got) != 2 {
		t.Fatalf("events = %+v, want two step lines", got)
	}
	first, second := got[0], got[1]
	if first.Kind != "step" || first.RunID != "run-1" || first.Seq != 1 || first.Command != "tap" || !first.Ok || first.Detail != "tapped" {
		t.Errorf("first = %+v", first)
	}
	if second.Seq != 2 || second.Command != "assert" || second.Ok || second.Detail != "" {
		t.Errorf("second = %+v", second)
	}
}

// readStepEvents serves the events feed of v until its feed closes and
// returns every line, decoded as a step event, so a line of another kind
// shows through its Kind.
func readStepEvents(t *testing.T, v *fakeViewer) []stepEventLine {
	t.Helper()
	srv, err := New(Config{Source: v, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: []RouteGroup{ReadRoutes}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, routeEvents, nil)
	req.Host = srv.hostPort()
	req.Header.Set(tokenHeader, authorize(t, srv))

	srv.Handler().ServeHTTP(rec, req)

	var got []stepEventLine
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		var l stepEventLine
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
	v.lines = []runfolder.StepLine{{RunID: "run-1", Seq: 5, Command: "tap", Phase: runfolder.StepPhase("end"), Ok: true}}
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
