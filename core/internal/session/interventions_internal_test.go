package session

import (
	"fmt"
	"slices"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
)

func TestInterventions_AHostThatNeverDrainsKeepsOnlyTheNewestEntries(t *testing.T) {
	s := &Session{deps: Deps{Clock: clock.SystemClock{}}}
	const recorded = maxPendingInterventions + 10
	for i := range recorded {
		s.recordIntervention(InterventionViewerAttached, fmt.Sprintf("client-%d", i))
	}

	got := s.Interventions()

	if len(got) != maxPendingInterventions {
		t.Fatalf("kept %d entries, want %d", len(got), maxPendingInterventions)
	}
	if got[0].Client != "client-10" || got[len(got)-1].Client != fmt.Sprintf("client-%d", recorded-1) {
		t.Fatalf("kept %s through %s, want the newest entries in order", got[0].Client, got[len(got)-1].Client)
	}
}

type listRecorder struct{ lines []runfolder.InterventionLine }

func (r *listRecorder) AppendIntervention(line runfolder.InterventionLine) error {
	r.lines = append(r.lines, line)
	return nil
}

func (r *listRecorder) clients() []string {
	out := make([]string, 0, len(r.lines))
	for _, l := range r.lines {
		out = append(out, l.Client)
	}
	return out
}

func newInterventionSession() *Session {
	return &Session{deps: Deps{Clock: clock.SystemClock{}, Logger: logging.NoopLogger{}}}
}

func TestInterventions_EntriesBeforeTheFirstRecorderReachItInOrderThenFollowLive(t *testing.T) {
	s := newInterventionSession()
	s.recordIntervention(InterventionViewerAttached, "early-1")
	s.recordIntervention(InterventionViewerAttached, "early-2")
	recorder := &listRecorder{}

	s.SetInterventionRecorder(recorder)
	s.recordIntervention(InterventionViewerDetached, "live")

	want := []string{"early-1", "early-2", "live"}
	if got := recorder.clients(); !slices.Equal(got, want) {
		t.Fatalf("recorded clients = %v, want %v", got, want)
	}
}

func TestInterventions_OnlyTheNewestUnrecordedEntriesAreKept(t *testing.T) {
	s := newInterventionSession()
	const recorded = maxUnrecordedInterventions + 5
	for i := range recorded {
		s.recordIntervention(InterventionViewerAttached, fmt.Sprintf("client-%d", i))
	}
	recorder := &listRecorder{}

	s.SetInterventionRecorder(recorder)

	got := recorder.clients()
	if len(got) != maxUnrecordedInterventions || got[0] != "client-5" {
		t.Fatalf("flushed %d entries starting at %q, want %d starting at client-5", len(got), got[0], maxUnrecordedInterventions)
	}
}

func TestInterventions_AnEntryAfterTheHostDecidedNeverReachesALaterRecorder(t *testing.T) {
	s := newInterventionSession()
	s.SetInterventionRecorder(nil)
	s.recordIntervention(InterventionViewerAttached, "between-runs")
	recorder := &listRecorder{}

	s.SetInterventionRecorder(recorder)

	if got := recorder.clients(); len(got) != 0 {
		t.Fatalf("recorded %v, want nothing from before the run opened", got)
	}
}
