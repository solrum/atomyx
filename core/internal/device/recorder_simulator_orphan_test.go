package device

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

const orphanTestGrace = 50 * time.Millisecond

type hostProcess struct {
	pid, ppid int
	command   string
	// ignoresInterrupt keeps the process alive after SIGINT.
	ignoresInterrupt bool
}

// processTable plays the host process list for the orphan reaper. ps
// prints it, and kill removes a process the way the signal would.
type processTable struct {
	h *recorderHarness

	mu    sync.Mutex
	procs []hostProcess
}

func (p *processTable) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
	switch name {
	case "ps":
		p.mu.Lock()
		defer p.mu.Unlock()
		var out strings.Builder
		for _, proc := range p.procs {
			fmt.Fprintf(&out, "%5d %5d %s\n", proc.pid, proc.ppid, proc.command)
		}
		return process.Result{Stdout: out.String()}, nil
	case "kill":
		p.h.log(strings.Join(append([]string{name}, args...), " "))
		pid, _ := strconv.Atoi(args[1])
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, proc := range p.procs {
			if proc.pid == pid && (args[0] != "-INT" || !proc.ignoresInterrupt) {
				p.procs = append(p.procs[:i], p.procs[i+1:]...)
				break
			}
		}
		return process.Result{}, nil
	}
	return p.h.Run(ctx, name, args...)
}

func (p *processTable) has(pid int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, proc := range p.procs {
		if proc.pid == pid {
			return true
		}
	}
	return false
}

func recordVideoCommand(udid string) string {
	return "/Library/Developer/PrivateFrameworks/CoreSimulator.framework/Versions/A/Resources/bin/simctl io " + udid + " recordVideo --codec hevc --force /tmp/x.mp4"
}

func startWithTable(t *testing.T, h *recorderHarness, table *processTable) (Recording, error) {
	t.Helper()
	h.stageSimulator(1)
	deps := RecorderDeps{Runner: table, Background: harnessBackground{h}}
	recorder := simulatorRecorder{deps: deps, wait: h.wait, orphanGrace: orphanTestGrace}
	return recorder.Start(context.Background(), simulatorDesc, t.TempDir(), "video-1")
}

func TestSimulatorRecorder_StartWithoutALeftoverRecorderKillsNothing(t *testing.T) {
	h := newRecorderHarness(t)
	table := &processTable{h: h, procs: []hostProcess{{pid: 10, ppid: 1, command: "/usr/sbin/cfprefsd agent"}}}

	rec, err := startWithTable(t, h, table)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()

	if h.count("kill") != 0 {
		t.Fatalf("killed a process with no leftover recorder: %q", h.snapshot())
	}
}

func TestSimulatorRecorder_StartReapsAnOrphanedRecorderBeforeRecording(t *testing.T) {
	h := newRecorderHarness(t)
	table := &processTable{h: h, procs: []hostProcess{{pid: 77, ppid: 1, command: recordVideoCommand(simulatorDesc.ID)}}}

	rec, err := startWithTable(t, h, table)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()

	if table.has(77) {
		t.Fatal("the orphaned recorder is still running")
	}
	if h.indexOf("kill -INT 77") > h.indexOf("start xcrun simctl io "+simulatorDesc.ID+" recordVideo") {
		t.Fatal("the new recorder started before the orphan was ended")
	}
	if h.count("kill -KILL") != 0 {
		t.Fatal("an orphan that exits on SIGINT was killed")
	}
}

func TestSimulatorRecorder_StartKillsAnOrphanThatIgnoresTheInterrupt(t *testing.T) {
	h := newRecorderHarness(t)
	table := &processTable{h: h, procs: []hostProcess{{pid: 77, ppid: 1, command: recordVideoCommand(simulatorDesc.ID), ignoresInterrupt: true}}}

	rec, err := startWithTable(t, h, table)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()

	if table.has(77) {
		t.Fatal("the orphaned recorder is still running")
	}
	h.indexOf("kill -KILL 77")
}

func TestSimulatorRecorder_StartLeavesARecorderWithALiveParentAndFails(t *testing.T) {
	h := newRecorderHarness(t)
	table := &processTable{h: h, procs: []hostProcess{{pid: 77, ppid: 4242, command: recordVideoCommand(simulatorDesc.ID)}}}

	rec, err := startWithTable(t, h, table)

	if err == nil || rec != nil {
		t.Fatalf("Start = %v, %v, want an error", rec, err)
	}
	if !strings.Contains(err.Error(), "another process (pid 77) is already recording simulator "+simulatorDesc.ID) {
		t.Fatalf("error = %q, want it to name the other recorder", err)
	}
	if !table.has(77) || h.count("kill") != 0 {
		t.Fatal("a recorder with a live parent was signalled")
	}
	if h.count("start") != 0 {
		t.Fatal("a recorder started despite the conflict")
	}
}

func TestSimulatorRecorder_StartLeavesARecorderOfAnotherSimulator(t *testing.T) {
	h := newRecorderHarness(t)
	table := &processTable{h: h, procs: []hostProcess{{pid: 88, ppid: 1, command: recordVideoCommand("OTHER-UDID")}}}

	rec, err := startWithTable(t, h, table)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()

	if !table.has(88) {
		t.Fatal("a recorder of another simulator was ended")
	}
}

func TestParseLeftoverRecorder_MatchesXcrunAndSimctlCommandLines(t *testing.T) {
	for _, command := range []string{
		"xcrun simctl io UDID-1 recordVideo --force /tmp/a.mp4",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/simctl io UDID-1 recordVideo /tmp/a.mp4",
	} {
		rec, ok := parseLeftoverRecorder("  901     1 "+command, "UDID-1")
		if !ok || rec.pid != 901 || rec.ppid != 1 {
			t.Fatalf("parse(%q) = %v, %v", command, rec, ok)
		}
	}
	if _, ok := parseLeftoverRecorder("  901     1 simctl io UDID-1 screenshot /tmp/a.png", "UDID-1"); ok {
		t.Fatal("matched a screenshot command")
	}
}
