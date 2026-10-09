package shard

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunDoesNotStopAWorkerThatAlreadyEndedWhenTheContextEnds(t *testing.T) {
	aLost := make(chan struct{})
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	ctx, cancel := context.WithCancel(context.Background())
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: losing(aLost), OnWait: cancel},
		"b": {ReadyAfter: aLost, Handle: func(p string) Line { <-gate; return passing(p) }},
	})

	outcome := Run(ctx, Options{
		Launcher: launcher, Devices: []Device{{ID: "a"}, {ID: "b"}}, Paths: paths(1),
		Args: func(Device) []string { return nil }, StopGrace: time.Second,
	})

	if !outcome.Interrupted || !launcher.Stopped("b") {
		t.Errorf("interrupted %v, b stopped %v, want the running worker stopped", outcome.Interrupted, launcher.Stopped("b"))
	}
	if launcher.Stopped("a") {
		t.Error("the interrupt stopped worker a after it had ended and been reaped")
	}
}

func TestRunHandsARequeuedEntryToAWorkerThatWentIdleWhileAnotherWasStillRunning(t *testing.T) {
	const settle = 100 * time.Millisecond
	bClosed, aGot := make(chan struct{}), make(chan struct{})
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: func(p string) Line {
			close(aGot)
			select {
			case <-bClosed:
			case <-time.After(settle):
			}
			line := entryLine(p, "failed")
			line.DeviceLost = true
			return line
		}},
		"b": {ReadyAfter: aGot, Handle: passing, OnWait: func() { close(bClosed) }},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b"}, paths(2))

	if got := launcher.Handled("b"); len(got) != 2 || got[0] != paths(2)[1] || got[1] != paths(2)[0] {
		t.Fatalf("worker b ran %v, want its own entry and then the one worker a lost", got)
	}
	if len(outcome.Entries) != 2 || len(outcome.Unrun) != 0 || outcome.Entries[0].Status != "passed" {
		t.Errorf("entries %+v, unrun %v, want both entries passed", outcome.Entries, outcome.Unrun)
	}
}

func TestRunSumsTheDurationOfEveryAttemptOfARequeuedEntry(t *testing.T) {
	aLost := make(chan struct{})
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: losing(aLost)},
		"b": {ReadyAfter: aLost, Handle: passing},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b"}, paths(1))

	if len(outcome.Entries) != 1 || len(outcome.Entries[0].Attempts) != 2 {
		t.Fatalf("entries = %+v, want one entry with two attempts", outcome.Entries)
	}
	if got := outcome.Entries[0].DurationMs; got != 20 {
		t.Errorf("entry duration = %d ms, want 20 for two attempts of 10 ms", got)
	}
}

func TestRunKeepsTheLostAttemptAndAddsAFailedOneWhenTheSecondWorkerVanishes(t *testing.T) {
	aLost := make(chan struct{})
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: losing(aLost)},
		"b": {ReadyAfter: aLost, Vanish: func() {}},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b"}, paths(1))

	if len(outcome.Entries) != 1 {
		t.Fatalf("entries = %+v, want the abandoned entry", outcome.Entries)
	}
	entry := outcome.Entries[0]
	if entry.Status != "failed" || entry.Error == nil || !strings.Contains(*entry.Error, "worker ended") || entry.DurationMs != 10 {
		t.Errorf("entry = %+v, want a failed entry that names the vanished worker", entry)
	}
	if len(entry.Attempts) != 2 {
		t.Fatalf("attempts = %+v, want the lost attempt and a synthetic one", entry.Attempts)
	}
	kept, synthetic := entry.Attempts[0], entry.Attempts[1]
	if kept.DeviceID != "a" || kept.Number != 1 || synthetic.Number != 2 || synthetic.Status != "failed" || synthetic.Error == nil {
		t.Errorf("attempts = %+v, want attempt 1 kept from device a and attempt 2 failed", entry.Attempts)
	}
}

func TestRunRejectsAReportForAnotherPathAndRunsTheEntryOnAnotherWorker(t *testing.T) {
	entries := paths(1)
	cases := map[string]func(string) Line{
		"another path": func(string) Line { return passing("/flows/other.yml") },
		"no attempts": func(p string) Line {
			return Line{Kind: LineEntry, Entry: &Entry{Path: p, Kind: "script", Status: "passed"}}
		},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			aGot := make(chan struct{})
			launcher := NewFakeLauncher(map[string]*FakeScript{
				"a": {Handle: func(p string) Line { close(aGot); return bad(p) }},
				"b": {Handle: passing, ReadyAfter: aGot},
			})

			outcome, warnings := runShard(t, launcher, []string{"a", "b"}, entries)

			if !launcher.Stopped("a") {
				t.Error("the worker with the invalid report was not stopped")
			}
			if len(outcome.Entries) != 1 || outcome.Entries[0].Path != entries[0] || outcome.Entries[0].Attempts[0].DeviceID != "b" {
				t.Errorf("entries = %+v, want the entry run by worker b under its own path", outcome.Entries)
			}
			if len(warnings) == 0 || !strings.Contains(warnings[0], "invalid report") {
				t.Errorf("warnings = %v, want one naming the invalid report", warnings)
			}
		})
	}
}
