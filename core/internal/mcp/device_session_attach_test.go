package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
)

// shortTempDir returns a temporary directory under a short path, so a
// holder socket placed beneath it stays within the unix path limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func (f *lifecycleFixture) holderSocket() string {
	return f.leases.SocketPathFor("android", "A")
}

// viewer connects to the holder and returns once the holder has seen it
// attach.
func (f *lifecycleFixture) viewer(name string) *sessiontest.Viewer {
	f.t.Helper()
	return sessiontest.DialViewer(f.t, f.holderSocket(), name)
}

func (f *lifecycleFixture) interventionLines(runID string) []runfolder.InterventionLine {
	f.t.Helper()
	body, err := os.ReadFile(filepath.Join(runfolder.RunDir(f.root, runID), runfolder.InterventionsFileName))
	if err != nil {
		f.t.Fatalf("reading the interventions file: %v", err)
	}
	var lines []runfolder.InterventionLine
	for _, raw := range splitLines(body) {
		var line runfolder.InterventionLine
		if err := json.Unmarshal(raw, &line); err != nil {
			f.t.Fatalf("decoding %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

func splitLines(body []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range body {
		if b == '\n' {
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	return out
}

func TestDeviceSessionSocket_TreeFreshFollowsTheHolderState(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	c := f.viewer("atomyx inspect")

	if err := c.TreeFresh(); err != nil {
		t.Fatalf("fresh while the session is idle: %v", err)
	}

	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}
	leave, err := f.devices.EnterCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := codeOfErr(c.TreeFresh()); got != diag.CodeHolderBusy {
		t.Fatalf("fresh during a call = %q, want %s", got, diag.CodeHolderBusy)
	}
	leave()
	f.devices.EndCall()
	if err := c.TreeFresh(); err != nil {
		t.Fatalf("fresh after the call ended: %v", err)
	}

	if err := f.devices.BeginCall("start_run"); err != nil {
		t.Fatal(err)
	}
	started := f.startRun()
	f.devices.EndCall()
	if got := codeOfErr(c.TreeFresh()); got != diag.CodeHolderBusy {
		t.Fatalf("fresh in an open run with no call in flight = %q, want %s", got, diag.CodeHolderBusy)
	}

	if err := f.devices.BeginCall("finish_run"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Finish(context.Background(), "passed", nil); err != nil {
		t.Fatal(err)
	}
	f.devices.EndCall()
	if err := c.TreeFresh(); err != nil {
		t.Fatalf("fresh after run %s was finished: %v", started.Run.ID, err)
	}
}

func TestDeviceSessionSocket_AViewerAttachingDuringAnOpenRunIsRecordedInItsFolder(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	started := f.startRun()

	f.viewer("atomyx inspect")

	lines := f.interventionLines(started.Run.ID)
	if len(lines) != 1 || lines[0].Kind != "viewerAttached" || lines[0].Client != "atomyx inspect" {
		t.Fatalf("interventions = %+v, want one viewerAttached by atomyx inspect", lines)
	}
}

func TestDeviceSessionSocket_ARunOpenedBeforeTheDeviceWasSelectedStillRecordsInterventions(t *testing.T) {
	f := newLifecycleFixture(t)
	started := f.startRun()
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}

	f.viewer("atomyx inspect")

	if lines := f.interventionLines(started.Run.ID); len(lines) != 1 {
		t.Fatalf("interventions = %+v, want the viewer recorded in the run opened first", lines)
	}
}

func TestDeviceSessionSocket_AFinishedRunStopsReceivingInterventions(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	started := f.startRun()
	if _, err := f.runs.Finish(context.Background(), "passed", nil); err != nil {
		t.Fatal(err)
	}

	f.viewer("atomyx inspect")

	f.devices.recMu.Lock()
	routed := f.devices.recorder
	f.devices.recMu.Unlock()
	if routed != nil {
		t.Fatalf("recorder = %v after the run finished, want none", routed)
	}
	if _, err := os.Stat(filepath.Join(runfolder.RunDir(f.root, started.Run.ID), runfolder.InterventionsFileName)); !os.IsNotExist(err) {
		t.Fatalf("interventions file after finish: %v, want none", err)
	}
}

func TestDeviceSessionSocket_AScriptThatIsItsOwnRunRecordsInterventionsInItsFolder(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}

	_, err := f.runs.RunScript(context.Background(), "own", nil, func(scriptTarget) (runner.ScriptResult, error) {
		f.viewer("atomyx inspect")
		return runner.ScriptResult{Ok: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	meta := f.onlyRun()
	if lines := f.interventionLines(meta.RunID); len(lines) != 1 || lines[0].Kind != "viewerAttached" {
		t.Fatalf("interventions = %+v, want one viewerAttached in the script's own run", lines)
	}
	f.devices.recMu.Lock()
	routed := f.devices.recorder
	f.devices.recMu.Unlock()
	if routed != nil {
		t.Fatalf("recorder = %v after the script ended, want none", routed)
	}
}

func TestDeviceSessionSocket_AScriptThatIsItsOwnRunNeverTakesOverAnOpenRunsInterventions(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	started := f.startRun()

	_, err := f.runs.RunScript(context.Background(), "inside", nil, func(scriptTarget) (runner.ScriptResult, error) {
		return runner.ScriptResult{Ok: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	f.viewer("atomyx inspect")
	if lines := f.interventionLines(started.Run.ID); len(lines) != 1 {
		t.Fatalf("interventions = %+v, want the open run to keep receiving them", lines)
	}
}

func TestDeviceSessionSocket_AScriptEndingNeverClearsTheRouteOfARunStartedMeanwhile(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	var started startedRun

	_, err := f.runs.RunScript(context.Background(), "own", nil, func(scriptTarget) (runner.ScriptResult, error) {
		started = f.startRun()
		return runner.ScriptResult{Ok: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	f.viewer("atomyx inspect")
	if lines := f.interventionLines(started.Run.ID); len(lines) != 1 {
		t.Fatalf("interventions = %+v, want the run started during the script to keep receiving them", lines)
	}
}

func TestRunSessionRoute_AConditionalRouteLeavesARoutedRunAlone(t *testing.T) {
	var recorders []session.InterventionRecorder
	s := &runSession{setInterventionRecorder: func(r session.InterventionRecorder) { recorders = append(recorders, r) }}
	open, other := &runfolder.Writer{}, &runfolder.Writer{}

	if !s.route(open, true) {
		t.Fatal("an unconditional route did not take over")
	}
	if s.route(other, false) {
		t.Fatal("a conditional route took over a routed run")
	}
	s.unroute(other)
	if len(recorders) != 1 || recorders[0] != session.InterventionRecorder(open) {
		t.Fatalf("recorders set = %v, want only the open run", recorders)
	}

	s.unroute(open)
	if len(recorders) != 2 || recorders[1] != nil {
		t.Fatalf("recorders set = %v, want the open run cleared last", recorders)
	}
	if !s.route(other, false) {
		t.Fatal("a conditional route did not take over a free route")
	}
}
