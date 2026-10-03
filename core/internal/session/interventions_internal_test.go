package session

import (
	"fmt"
	"testing"

	"dev.atomyx/internal/clock"
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
