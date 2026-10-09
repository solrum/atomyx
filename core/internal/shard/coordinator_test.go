package shard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func entryLine(path, status string) Line {
	return Line{Kind: LineEntry, Entry: &Entry{Path: path, Kind: "script", Status: status, DurationMs: 10, Attempts: []Attempt{{Number: 1, Status: status, DurationMs: 10}}}}
}

func passing(path string) Line { return entryLine(path, "passed") }

func paths(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("/flows/%02d.yml", i)
	}
	return out
}

func runShard(t *testing.T, launcher Launcher, devices []string, entries []string) (Outcome, []string) {
	t.Helper()
	var warnings []string
	var list []Device
	for _, id := range devices {
		list = append(list, Device{ID: id})
	}
	outcome := Run(context.Background(), Options{
		Launcher: launcher, Devices: list, Paths: entries,
		Args: func(Device) []string { return nil }, StopGrace: time.Second,
		Warn: func(s string) { warnings = append(warnings, s) },
	})
	return outcome, warnings
}

func TestRunGivesEveryEntryToExactlyOneWorkerAndKeepsSuiteOrder(t *testing.T) {
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: passing}, "b": {Handle: passing}, "c": {Handle: passing},
	})
	entries := paths(9)

	outcome, warnings := runShard(t, launcher, []string{"a", "b", "c"}, entries)

	if len(warnings) != 0 || len(outcome.Unrun) != 0 {
		t.Fatalf("warnings %v, unrun %v", warnings, outcome.Unrun)
	}
	var got []string
	for _, entry := range outcome.Entries {
		got = append(got, entry.Path)
		if len(entry.Attempts) != 1 || entry.Attempts[0].DeviceID == "" || !strings.HasPrefix(entry.Attempts[0].WorkerRunID, "run-") {
			t.Errorf("entry %s attempts = %+v, want the device and the worker run stamped", entry.Path, entry.Attempts)
		}
	}
	if !slices.Equal(got, entries) {
		t.Errorf("entries = %v, want suite order %v", got, entries)
	}
	var ran []string
	for _, id := range []string{"a", "b", "c"} {
		ran = append(ran, launcher.Handled(id)...)
	}
	slices.Sort(ran)
	if !slices.Equal(ran, entries) {
		t.Errorf("workers ran %v, want every entry exactly once", ran)
	}
}

func TestRunGivesTheNextEntryToWhicheverWorkerIsFree(t *testing.T) {
	entries := paths(6)
	release := make(chan struct{})
	var fastDone sync.WaitGroup
	fastDone.Add(len(entries) - 1)
	slowStarted := make(chan struct{})
	var startOnce sync.Once
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"slow": {Handle: func(path string) Line {
			startOnce.Do(func() { close(slowStarted) })
			<-release
			return passing(path)
		}},
		"fast": {ReadyAfter: slowStarted, Handle: func(path string) Line { defer fastDone.Done(); return passing(path) }},
	})
	go func() { fastDone.Wait(); close(release) }()

	outcome, _ := runShard(t, launcher, []string{"slow", "fast"}, entries)

	if len(outcome.Entries) != len(entries) || len(launcher.Handled("slow")) != 1 || len(launcher.Handled("fast")) != len(entries)-1 {
		t.Errorf("slow ran %v, fast ran %v, want the fast worker to take the rest", launcher.Handled("slow"), launcher.Handled("fast"))
	}
}

// losing answers every path as a failed entry whose device was lost, and
// closes lost so a later worker can start only after the loss happened.
func losing(lost chan struct{}) func(string) Line {
	var once sync.Once
	return func(path string) Line {
		line := entryLine(path, "failed")
		line.DeviceLost = true
		once.Do(func() { close(lost) })
		return line
	}
}

func TestRunRequeuesTheEntryOfALostDeviceOnceToAnotherWorker(t *testing.T) {
	aLost := make(chan struct{})
	lose := losing(aLost)
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: func(p string) Line {
			if strings.HasSuffix(p, "01.yml") {
				return lose(p)
			}
			return passing(p)
		}},
		"b": {ReadyAfter: aLost, Handle: passing},
	})

	outcome, warnings := runShard(t, launcher, []string{"a", "b"}, paths(4))

	if len(outcome.Unrun) != 0 || len(outcome.Entries) != 4 {
		t.Fatalf("entries %d, unrun %v", len(outcome.Entries), outcome.Unrun)
	}
	var lostEntry *Entry
	for i := range outcome.Entries {
		if strings.HasSuffix(outcome.Entries[i].Path, "01.yml") {
			lostEntry = &outcome.Entries[i]
		}
	}
	if lostEntry == nil || lostEntry.Status != "passed" || len(lostEntry.Attempts) != 2 {
		t.Fatalf("lost entry = %+v, want a pass after two attempts", lostEntry)
	}
	first, second := lostEntry.Attempts[0], lostEntry.Attempts[1]
	if first.DeviceID != "a" || first.Status != "failed" || first.Number != 1 || second.DeviceID != "b" || second.Status != "passed" || second.Number != 2 || first.WorkerRunID == second.WorkerRunID {
		t.Errorf("attempts = %+v, want a failed attempt on a then a passed attempt on b in another worker run", lostEntry.Attempts)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "device a was lost") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestRunRequeuesALostEntryOnlyOnce(t *testing.T) {
	aLost, bLost := make(chan struct{}), make(chan struct{})
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: losing(aLost)},
		"b": {ReadyAfter: aLost, Handle: losing(bLost)},
		"c": {ReadyAfter: bLost, Handle: passing},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b", "c"}, paths(1))

	if len(outcome.Entries) != 1 || outcome.Entries[0].Status != "failed" || len(outcome.Entries[0].Attempts) != 2 {
		t.Fatalf("entries = %+v, want one failed entry with two attempts", outcome.Entries)
	}
	if got := launcher.Handled("c"); len(got) != 0 {
		t.Errorf("worker c ran %v, want the entry not to be requeued a second time", got)
	}
}

func TestRunDropsAWorkerThatReportsDeviceHeld(t *testing.T) {
	held := Line{Kind: LineError, Code: "DEVICE_HELD", Message: "held by atomyx inspect"}
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {ReadyLine: &held}, "b": {Handle: passing},
	})

	outcome, warnings := runShard(t, launcher, []string{"a", "b"}, paths(3))

	if len(outcome.Entries) != 3 || len(launcher.Handled("a")) != 0 || len(launcher.Handled("b")) != 3 {
		t.Errorf("entries %d, a ran %v, b ran %v", len(outcome.Entries), launcher.Handled("a"), launcher.Handled("b"))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "device a dropped") || !strings.Contains(warnings[0], "DEVICE_HELD") {
		t.Errorf("warnings = %v, want one naming device a and DEVICE_HELD", warnings)
	}
}

func TestRunLeavesEntriesUnrunWhenEveryWorkerIsGone(t *testing.T) {
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {StartErr: errors.New("no such file")}, "b": {ReadyLine: &Line{Kind: LineError, Code: "DEVICE_HELD"}},
	})

	outcome, warnings := runShard(t, launcher, []string{"a", "b"}, paths(3))

	if len(outcome.Entries) != 0 || len(outcome.Unrun) != 3 {
		t.Errorf("entries %d, unrun %v, want all three unrun", len(outcome.Entries), outcome.Unrun)
	}
	if len(warnings) != 3 || !strings.Contains(warnings[2], "3 entries were not run") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestRunLeavesTheRestUnrunWhenTheOnlyWorkerLosesItsDevice(t *testing.T) {
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: func(p string) Line { l := entryLine(p, "failed"); l.DeviceLost = true; return l }},
	})

	outcome, _ := runShard(t, launcher, []string{"a"}, paths(3))

	if len(outcome.Entries) != 1 || outcome.Entries[0].Status != "failed" || len(outcome.Unrun) != 2 {
		t.Errorf("entries %+v, unrun %v, want the lost entry failed and two unrun", outcome.Entries, outcome.Unrun)
	}
}

func TestRunStopsEveryWorkerWhenTheContextEnds(t *testing.T) {
	started := make(chan struct{}, 2)
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	block := func(path string) Line { started <- struct{}{}; <-gate; return passing(path) }
	launcher := NewFakeLauncher(map[string]*FakeScript{"a": {Handle: block}, "b": {Handle: block}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; <-started; cancel() }()

	outcome := Run(ctx, Options{
		Launcher: launcher, Devices: []Device{{ID: "a"}, {ID: "b"}}, Paths: paths(4),
		Args: func(Device) []string { return nil }, StopGrace: time.Second,
	})

	if !outcome.Interrupted || !launcher.Stopped("a") || !launcher.Stopped("b") {
		t.Errorf("interrupted %v, stopped a=%v b=%v, want both workers stopped", outcome.Interrupted, launcher.Stopped("a"), launcher.Stopped("b"))
	}
}

func TestRunDoesNotRequeueAnEntryThatPassedBeforeTheDeviceWasLost(t *testing.T) {
	aDone := make(chan struct{})
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Handle: func(p string) Line { l := passing(p); l.DeviceLost = true; close(aDone); return l }},
		"b": {ReadyAfter: aDone, Handle: passing},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b"}, paths(1))

	if len(outcome.Entries) != 1 || outcome.Entries[0].Status != "passed" || len(outcome.Entries[0].Attempts) != 1 {
		t.Errorf("entries = %+v, want the passed entry kept as it is", outcome.Entries)
	}
}

func TestRunRequeuesTheEntryOfAWorkerThatVanishedWithoutReporting(t *testing.T) {
	gone := make(chan struct{})
	launcher := NewFakeLauncher(map[string]*FakeScript{
		"a": {Vanish: func() { close(gone) }},
		"b": {ReadyAfter: gone, Handle: passing},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b"}, paths(2))

	if len(outcome.Unrun) != 0 || len(outcome.Entries) != 2 {
		t.Fatalf("entries %d, unrun %v", len(outcome.Entries), outcome.Unrun)
	}
	for _, entry := range outcome.Entries {
		if entry.Status != "passed" || entry.Attempts[0].DeviceID != "b" {
			t.Errorf("entry %s = %+v, want it run by the surviving worker", entry.Path, entry)
		}
	}
}
