package session_test

import (
	"context"
	"errors"
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
// has seen it attach, because a served request is answered only after the
// attach callback ran.
func attachNamed(t *testing.T, path, name string) *attach.Client {
	t.Helper()
	c, err := attach.Dial(context.Background(), attach.ClientConfig{Path: path, Client: name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Call(context.Background(), attach.TypeStatus, nil, nil); err != nil {
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
