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

// scripted describes how the fake worker of one device behaves.
type scripted struct {
	startErr  error
	readyLine *Line
	// readyAfter, when set, holds the ready line back until it is closed.
	readyAfter <-chan struct{}
	// handle answers one path. It may block to model a slow device.
	handle func(path string) Line
	// vanish, when set, makes the worker call it and exit on its first
	// path without answering, like a crashed process.
	vanish func()
}

type fakeLauncher struct {
	mu      sync.Mutex
	devices map[string]*scripted
	seen    map[string][]string
	stopped map[string]bool
}

func newFakeLauncher(devices map[string]*scripted) *fakeLauncher {
	return &fakeLauncher{devices: devices, seen: map[string][]string{}, stopped: map[string]bool{}}
}

func (l *fakeLauncher) Launch(_ context.Context, spec WorkerSpec) (Worker, error) {
	behavior := l.devices[spec.Device.ID]
	if behavior.startErr != nil {
		return nil, behavior.startErr
	}
	w := &fakeWorker{id: spec.Device.ID, launcher: l, behavior: behavior, lines: make(chan Line), in: make(chan string), quit: make(chan struct{})}
	go w.run()
	return w, nil
}

func (l *fakeLauncher) handled(id string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seen[id])
}

type fakeWorker struct {
	id       string
	launcher *fakeLauncher
	behavior *scripted
	lines    chan Line
	in       chan string
	quit     chan struct{}
	once     sync.Once
}

func (w *fakeWorker) run() {
	defer close(w.lines)
	if w.behavior.readyAfter != nil {
		select {
		case <-w.behavior.readyAfter:
		case <-w.quit:
			return
		}
	}
	if w.behavior.readyLine != nil {
		w.lines <- *w.behavior.readyLine
		return
	}
	w.lines <- Line{Kind: LineReady, RunID: "run-" + w.id}
	for {
		select {
		case path, ok := <-w.in:
			if !ok {
				return
			}
			w.launcher.mu.Lock()
			w.launcher.seen[w.id] = append(w.launcher.seen[w.id], path)
			w.launcher.mu.Unlock()
			if w.behavior.vanish != nil {
				w.behavior.vanish()
				return
			}
			answer := make(chan Line, 1)
			go func() { answer <- w.behavior.handle(path) }()
			var line Line
			select {
			case line = <-answer:
			case <-w.quit:
				return
			}
			line.RunID = "run-" + w.id
			select {
			case w.lines <- line:
			case <-w.quit:
				return
			}
			if line.DeviceLost {
				return
			}
		case <-w.quit:
			return
		}
	}
}

func (w *fakeWorker) Lines() <-chan Line { return w.lines }
func (w *fakeWorker) Send(path string) error {
	select {
	case w.in <- path:
		return nil
	case <-w.quit:
		return errors.New("worker gone")
	}
}
func (w *fakeWorker) CloseInput() error {
	w.once.Do(func() { close(w.in) })
	return nil
}
func (w *fakeWorker) Stop(time.Duration) {
	w.launcher.mu.Lock()
	w.launcher.stopped[w.id] = true
	w.launcher.mu.Unlock()
	close(w.quit)
}
func (w *fakeWorker) Wait() Exit { return Exit{Stderr: "worker stderr"} }

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
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {handle: passing}, "b": {handle: passing}, "c": {handle: passing},
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
		ran = append(ran, launcher.handled(id)...)
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
	launcher := newFakeLauncher(map[string]*scripted{
		"slow": {handle: func(path string) Line {
			startOnce.Do(func() { close(slowStarted) })
			<-release
			return passing(path)
		}},
		"fast": {readyAfter: slowStarted, handle: func(path string) Line { defer fastDone.Done(); return passing(path) }},
	})
	go func() { fastDone.Wait(); close(release) }()

	outcome, _ := runShard(t, launcher, []string{"slow", "fast"}, entries)

	if len(outcome.Entries) != len(entries) || len(launcher.handled("slow")) != 1 || len(launcher.handled("fast")) != len(entries)-1 {
		t.Errorf("slow ran %v, fast ran %v, want the fast worker to take the rest", launcher.handled("slow"), launcher.handled("fast"))
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
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {handle: func(p string) Line {
			if strings.HasSuffix(p, "01.yml") {
				return lose(p)
			}
			return passing(p)
		}},
		"b": {readyAfter: aLost, handle: passing},
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
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {handle: losing(aLost)},
		"b": {readyAfter: aLost, handle: losing(bLost)},
		"c": {readyAfter: bLost, handle: passing},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b", "c"}, paths(1))

	if len(outcome.Entries) != 1 || outcome.Entries[0].Status != "failed" || len(outcome.Entries[0].Attempts) != 2 {
		t.Fatalf("entries = %+v, want one failed entry with two attempts", outcome.Entries)
	}
	if got := launcher.handled("c"); len(got) != 0 {
		t.Errorf("worker c ran %v, want the entry not to be requeued a second time", got)
	}
}

func TestRunDropsAWorkerThatReportsDeviceHeld(t *testing.T) {
	held := Line{Kind: LineError, Code: "DEVICE_HELD", Message: "held by atomyx inspect"}
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {readyLine: &held}, "b": {handle: passing},
	})

	outcome, warnings := runShard(t, launcher, []string{"a", "b"}, paths(3))

	if len(outcome.Entries) != 3 || len(launcher.handled("a")) != 0 || len(launcher.handled("b")) != 3 {
		t.Errorf("entries %d, a ran %v, b ran %v", len(outcome.Entries), launcher.handled("a"), launcher.handled("b"))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "device a dropped") || !strings.Contains(warnings[0], "DEVICE_HELD") {
		t.Errorf("warnings = %v, want one naming device a and DEVICE_HELD", warnings)
	}
}

func TestRunLeavesEntriesUnrunWhenEveryWorkerIsGone(t *testing.T) {
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {startErr: errors.New("no such file")}, "b": {readyLine: &Line{Kind: LineError, Code: "DEVICE_HELD"}},
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
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {handle: func(p string) Line { l := entryLine(p, "failed"); l.DeviceLost = true; return l }},
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
	launcher := newFakeLauncher(map[string]*scripted{"a": {handle: block}, "b": {handle: block}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; <-started; cancel() }()

	outcome := Run(ctx, Options{
		Launcher: launcher, Devices: []Device{{ID: "a"}, {ID: "b"}}, Paths: paths(4),
		Args: func(Device) []string { return nil }, StopGrace: time.Second,
	})

	if !outcome.Interrupted || !launcher.stopped["a"] || !launcher.stopped["b"] {
		t.Errorf("interrupted %v, stopped %v, want both workers stopped", outcome.Interrupted, launcher.stopped)
	}
}

func TestRunDoesNotRequeueAnEntryThatPassedBeforeTheDeviceWasLost(t *testing.T) {
	aDone := make(chan struct{})
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {handle: func(p string) Line { l := passing(p); l.DeviceLost = true; close(aDone); return l }},
		"b": {readyAfter: aDone, handle: passing},
	})

	outcome, _ := runShard(t, launcher, []string{"a", "b"}, paths(1))

	if len(outcome.Entries) != 1 || outcome.Entries[0].Status != "passed" || len(outcome.Entries[0].Attempts) != 1 {
		t.Errorf("entries = %+v, want the passed entry kept as it is", outcome.Entries)
	}
}

func TestRunRequeuesTheEntryOfAWorkerThatVanishedWithoutReporting(t *testing.T) {
	gone := make(chan struct{})
	launcher := newFakeLauncher(map[string]*scripted{
		"a": {vanish: func() { close(gone) }},
		"b": {readyAfter: gone, handle: passing},
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
