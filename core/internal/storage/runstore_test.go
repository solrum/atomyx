package storage

import (
	"errors"
	"testing"
)

func TestRunStoreStartCreatesActiveRunWithUniqueID(t *testing.T) {
	s := NewRunStore()
	source := "exploratory"
	result := s.Start(StartOptions{Name: "login flow", Source: &source})

	run := result.Run
	if run.Status != RunStatusRunning {
		t.Fatalf("Status: got %q, want %q", run.Status, RunStatusRunning)
	}
	if run.Name != "login flow" {
		t.Fatalf("Name: got %q", run.Name)
	}
	if run.Source != "exploratory" {
		t.Fatalf("Source: got %q", run.Source)
	}
	if run.ActionCount != 0 {
		t.Fatalf("ActionCount: got %d, want 0", run.ActionCount)
	}
	if len(run.Findings) != 0 {
		t.Fatalf("Findings: got %v, want empty", run.Findings)
	}
	if s.Current() != run {
		t.Fatal("Current: expected the started run")
	}
	if result.ErroredPredecessor != nil {
		t.Fatalf("ErroredPredecessor: got %v, want nil", result.ErroredPredecessor)
	}
}

func TestRunStoreStartDefaultsSourceToUnknown(t *testing.T) {
	s := NewRunStore()
	result := s.Start(StartOptions{Name: "x"})
	if result.Run.Source != "unknown" {
		t.Fatalf("Source: got %q, want %q", result.Run.Source, "unknown")
	}
}

func TestRunStoreIncrementActionsBumpsCounterOnActiveRun(t *testing.T) {
	s := NewRunStore()
	s.Start(StartOptions{Name: "x"})
	s.IncrementActions()
	s.IncrementActions()
	s.IncrementActions()
	if got := s.Current().ActionCount; got != 3 {
		t.Fatalf("ActionCount: got %d, want 3", got)
	}
}

func TestRunStoreIncrementActionsNoOpWhenNoActiveRun(t *testing.T) {
	s := NewRunStore()
	s.IncrementActions()
	if s.Current() != nil {
		t.Fatal("Current: expected nil")
	}
}

func TestRunStoreFinishMarksVerdictAndClearsCurrent(t *testing.T) {
	s := NewRunStore()
	s.Start(StartOptions{Name: "x"})
	finished := s.Finish(RunStatusPassed)
	if finished == nil {
		t.Fatal("Finish: expected a finished run")
	}
	if finished.Status != RunStatusPassed {
		t.Fatalf("Status: got %q, want %q", finished.Status, RunStatusPassed)
	}
	if finished.FinishedAt == nil {
		t.Fatal("FinishedAt: expected non-nil")
	}
	if s.Current() != nil {
		t.Fatal("Current: expected nil after Finish")
	}
}

func TestRunStoreFinishDefaultsToPassed(t *testing.T) {
	s := NewRunStore()
	s.Start(StartOptions{Name: "x"})
	finished := s.Finish("")
	if finished.Status != RunStatusPassed {
		t.Fatalf("Status: got %q, want %q", finished.Status, RunStatusPassed)
	}
}

func TestRunStoreFinishReturnsNilWhenNoActiveRun(t *testing.T) {
	s := NewRunStore()
	if got := s.Finish(RunStatusPassed); got != nil {
		t.Fatalf("Finish: got %v, want nil", got)
	}
}

func TestRunStoreFinishCalledTwiceIsNoOpOnSecondCall(t *testing.T) {
	s := NewRunStore()
	s.Start(StartOptions{Name: "x"})
	s.Finish(RunStatusPassed)
	if got := s.Finish(RunStatusPassed); got != nil {
		t.Fatalf("second Finish: got %v, want nil", got)
	}
}

func TestRunStoreStartingNewRunWhileOneActiveReturnsErroredPredecessor(t *testing.T) {
	s := NewRunStore()
	first := s.Start(StartOptions{Name: "first"}).Run
	second := s.Start(StartOptions{Name: "second"})

	if first.Status != RunStatusError {
		t.Fatalf("first.Status: got %q, want %q", first.Status, RunStatusError)
	}
	if first.FinishedAt == nil {
		t.Fatal("first.FinishedAt: expected non-nil")
	}
	if second.Run.Status != RunStatusRunning {
		t.Fatalf("second.Run.Status: got %q, want %q", second.Run.Status, RunStatusRunning)
	}
	if s.Current() != second.Run {
		t.Fatal("Current: expected the second run")
	}
	if second.ErroredPredecessor != first {
		t.Fatal("ErroredPredecessor: expected the first run")
	}
}

func TestRunStoreRecordBugAddsFindingToActiveRun(t *testing.T) {
	s := NewRunStore()
	s.Start(StartOptions{Name: "x"})
	bug, err := s.RecordBug(RecordBugOptions{
		Title:       "crash on login",
		Description: "tapping login crashes",
	})
	if err != nil {
		t.Fatalf("RecordBug: %v", err)
	}
	if bug.ID == "" {
		t.Fatal("bug.ID: expected non-empty")
	}
	if len(s.Current().Findings) != 1 {
		t.Fatalf("Findings: got %d, want 1", len(s.Current().Findings))
	}
	if got := s.Current().Findings[0].Title; got != "crash on login" {
		t.Fatalf("Findings[0].Title: got %q", got)
	}
}

func TestRunStoreRecordBugFailsWhenNoActiveRun(t *testing.T) {
	s := NewRunStore()
	_, err := s.RecordBug(RecordBugOptions{Title: "x", Description: "y"})
	if !errors.Is(err, ErrNoActiveRun) {
		t.Fatalf("RecordBug: got err %v, want ErrNoActiveRun", err)
	}
}

type fakeClock struct{ ms int64 }

func (c *fakeClock) NowMs() int64 { c.ms++; return c.ms }

func TestRunStoreBugIDsAreDeterministicWithinARun(t *testing.T) {
	s := NewRunStoreWithClock(&fakeClock{})
	result := s.Start(StartOptions{Name: "x"})
	first, err := s.RecordBug(RecordBugOptions{Title: "a", Description: "d"})
	if err != nil {
		t.Fatalf("RecordBug: %v", err)
	}
	second, err := s.RecordBug(RecordBugOptions{Title: "b", Description: "d"})
	if err != nil {
		t.Fatalf("RecordBug: %v", err)
	}
	if want := result.Run.ID + "-bug-1"; first.ID != want {
		t.Fatalf("first.ID: got %q, want %q", first.ID, want)
	}
	if want := result.Run.ID + "-bug-2"; second.ID != want {
		t.Fatalf("second.ID: got %q, want %q", second.ID, want)
	}
}

func TestRunStoreIsLiveOnlyForTheActiveRun(t *testing.T) {
	store := NewRunStoreWithClock(&fakeClock{})
	if store.IsLive("anything") {
		t.Fatal("an idle store reported a live run")
	}
	run := store.Start(StartOptions{Name: "n"}).Run
	if !store.IsLive(run.ID) || store.IsLive("other") {
		t.Fatal("the active run must be the only live run")
	}
	store.Finish(RunStatusPassed)
	if store.IsLive(run.ID) {
		t.Fatal("a finished run reported live")
	}
}
