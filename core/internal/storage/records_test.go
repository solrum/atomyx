package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunKeyAndBugKeyPrefixes(t *testing.T) {
	if got := RunKey("abc"); got != "runs/abc" {
		t.Fatalf("RunKey: got %q", got)
	}
	if got := BugKey("abc-bug-1"); got != "bugs/abc-bug-1" {
		t.Fatalf("BugKey: got %q", got)
	}
}

func TestPersistedRunRoundTripsThroughFileStorage(t *testing.T) {
	root := t.TempDir()
	fileStore, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	runStore := NewRunStoreWithClock(&fakeClock{})

	runStore.Start(StartOptions{ID: "run-login", Name: "login flow"})
	if _, err := runStore.RecordBug(RecordBugOptions{Title: "t", Description: "d"}); err != nil {
		t.Fatalf("RecordBug: %v", err)
	}
	finished := runStore.Finish(RunStatusPassed)

	summary := "looked good"
	persisted := PersistedRun{
		ID: finished.ID, Name: finished.Name, Source: finished.Source, Status: finished.Status,
		StartedAt: finished.StartedAt, FinishedAt: finished.FinishedAt,
		DurationMs: *finished.FinishedAt - finished.StartedAt, ActionCount: finished.ActionCount,
		Findings: finished.Findings, Summary: &summary,
	}
	if err := fileStore.Save(RunKey(finished.ID), persisted); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, "runs", finished.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected run record on disk at %s: %v", path, err)
	}

	var loaded PersistedRun
	found, err := fileStore.Load(RunKey(finished.ID), &loaded)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load: expected the run record to be found")
	}
	if loaded.ID != finished.ID || loaded.Status != RunStatusPassed || loaded.Summary == nil || *loaded.Summary != summary {
		t.Fatalf("loaded record mismatch: %+v", loaded)
	}
	if len(loaded.Findings) != 1 || loaded.Findings[0].Title != "t" {
		t.Fatalf("loaded findings mismatch: %+v", loaded.Findings)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, field := range []string{`"id"`, `"name"`, `"source"`, `"status"`, `"startedAt"`, `"finishedAt"`, `"durationMs"`, `"actionCount"`, `"findings"`, `"summary"`} {
		if !strings.Contains(string(body), field) {
			t.Fatalf("on-disk record missing field %s:\n%s", field, body)
		}
	}
}

func TestPersistedBugRoundTripsThroughFileStorage(t *testing.T) {
	root := t.TempDir()
	fileStore, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}

	bug := PersistedBug{
		ID:          "run-1-1-bug-1",
		RunID:       "run-1-1",
		RunName:     "login flow",
		Title:       "crash",
		Description: "tapping login crashes",
		Timestamp:   1000,
	}
	if err := fileStore.Save(BugKey(bug.ID), bug); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, "bugs", bug.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected bug record on disk at %s: %v", path, err)
	}

	var loaded PersistedBug
	found, err := fileStore.Load(BugKey(bug.ID), &loaded)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found || loaded != bug {
		t.Fatalf("loaded bug mismatch: found=%v got=%+v want=%+v", found, loaded, bug)
	}
}
