package session_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
)

type fakeRecorder struct {
	mu    sync.Mutex
	lines []runfolder.InterventionLine
	err   error
}

func (r *fakeRecorder) AppendIntervention(line runfolder.InterventionLine) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	return r.err
}

func (r *fakeRecorder) recorded() []runfolder.InterventionLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runfolder.InterventionLine(nil), r.lines...)
}

// attachNamed connects a viewer under name and returns once the holder
// has seen it attach, because the first observe request is answered only
// after the attach callback ran.
func attachNamed(t *testing.T, path, name string) *attach.Client {
	t.Helper()
	c, err := attach.Dial(context.Background(), attach.ClientConfig{Path: path, Client: name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Call(context.Background(), attach.TypeTreeLatest, nil, nil); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInterventions_AViewerAttachingAndDetachingIsRecordedOnceAndDrained(t *testing.T) {
	s, _, path := socketHolder(t)
	attachNamed(t, path, "atomyx inspect")

	got := s.Interventions()
	if len(got) != 1 || got[0].Kind != session.InterventionViewerAttached || got[0].Client != "atomyx inspect" || got[0].AtMs <= 0 {
		t.Fatalf("Interventions() = %+v, want one viewerAttached entry for atomyx inspect with a time", got)
	}
	if again := s.Interventions(); len(again) != 0 {
		t.Fatalf("second Interventions() = %+v, want none after the drain", again)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got = s.Interventions()
	if len(got) != 1 || got[0].Kind != session.InterventionViewerDetached || got[0].Client != "atomyx inspect" {
		t.Fatalf("Interventions() after close = %+v, want one viewerDetached entry", got)
	}
}

func TestInterventions_AnOpenRunRecorderReceivesEachEntryInOrderUntilItIsCleared(t *testing.T) {
	s, _, path := socketHolder(t)
	recorder := &fakeRecorder{}
	s.SetInterventionRecorder(recorder)

	attachNamed(t, path, "one")
	attachNamed(t, path, "two")
	s.SetInterventionRecorder(nil)
	attachNamed(t, path, "three")

	clients := []string{"one", "two"}
	lines := recorder.recorded()
	if len(lines) != len(clients) {
		t.Fatalf("recorded %+v, want %d lines", lines, len(clients))
	}
	for i, line := range lines {
		if line.Kind != session.InterventionViewerAttached || line.Client != clients[i] {
			t.Fatalf("line %d = %+v, want viewerAttached for %s", i, line, clients[i])
		}
	}
	if pending := s.Interventions(); len(pending) != 3 {
		t.Fatalf("Interventions() = %+v, want every entry kept for the drainer, including after the recorder was cleared", pending)
	}
}

func TestInterventions_ARecorderFailureNeverLosesThePendingEntry(t *testing.T) {
	s, _, path := socketHolder(t)
	s.SetInterventionRecorder(&fakeRecorder{err: errors.New("disk full")})

	attachNamed(t, path, "atomyx inspect")

	if got := s.Interventions(); len(got) != 1 {
		t.Fatalf("Interventions() = %+v, want the entry kept when the run folder write failed", got)
	}
}

func TestInterventions_AViewerAttachingBeforeTheHostSetsTheRecorderIsStillWrittenToTheRun(t *testing.T) {
	s, _, path := socketHolder(t)
	attachNamed(t, path, "early viewer")
	recorder := &fakeRecorder{}

	s.SetInterventionRecorder(recorder)

	lines := recorder.recorded()
	if len(lines) != 1 || lines[0].Kind != session.InterventionViewerAttached || lines[0].Client != "early viewer" {
		t.Fatalf("recorded %+v, want the viewerAttached entry of the early viewer", lines)
	}
}

func TestInterventions_ARecordedHostInterventionIsDrainedWithItsClient(t *testing.T) {
	s, _, _ := socketHolder(t)

	s.RecordIntervention(session.InterventionControlBorrowed, "atomyx run login.yml")

	got := s.Interventions()
	if len(got) != 1 || got[0].Kind != session.InterventionControlBorrowed || got[0].Client != "atomyx run login.yml" || got[0].AtMs <= 0 {
		t.Fatalf("Interventions() = %+v, want one controlBorrowed entry for the borrower's command", got)
	}
}

func TestInterventions_AnActIsRecordedAsAHumanActionWithItsResultAndWhoRanIt(t *testing.T) {
	dbg := &scriptedDebugger{}
	s, _, path := debuggableHolder(t, dbg)
	recorder := &fakeRecorder{}
	s.SetInterventionRecorder(recorder)
	s.EnterPause(testPause)

	client, err := attach.Dial(context.Background(), attach.ClientConfig{Path: path, Client: "atomyx inspect"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Call(context.Background(), attach.TypeActStep, attach.ActStep{Step: "tap: ok"}, nil); err != nil {
		t.Fatal(err)
	}

	var acts []runfolder.InterventionLine
	for _, line := range recorder.recorded() {
		if line.Kind == session.InterventionHumanAction {
			acts = append(acts, line)
		}
	}
	want := runfolder.InterventionLine{
		Kind: session.InterventionHumanAction, Client: "atomyx inspect", Step: "- tap: ok",
		Command: "tap", Outcome: "passed", DurationMs: 12, Artifacts: []string{"artifacts/a.png"},
	}
	if len(acts) != 1 || acts[0].AtMs <= 0 {
		t.Fatalf("recorded humanAction lines = %+v, want exactly one with a time", acts)
	}
	acts[0].AtMs = 0
	if !reflect.DeepEqual(acts[0], want) {
		t.Fatalf("humanAction line = %+v, want %+v", acts[0], want)
	}
}

func TestInterventions_ARefusedActIsNotRecorded(t *testing.T) {
	s, _, _ := debuggableHolder(t, &scriptedDebugger{})
	recorder := &fakeRecorder{}
	s.SetInterventionRecorder(recorder)

	if _, err := s.Act(context.Background(), "tap: ok"); err == nil {
		t.Fatal("an act on a run that is not paused succeeded")
	}
	for _, line := range recorder.recorded() {
		if line.Kind == session.InterventionHumanAction {
			t.Fatalf("a refused act was recorded: %+v", line)
		}
	}
}
