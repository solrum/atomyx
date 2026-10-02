package mcp

import (
	"os"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type runServer struct {
	root  string
	store storage.Storage
	cs    *mcp.ClientSession
	call  func(name string, args map[string]any) map[string]any
}

func newRunServer(t *testing.T) runServer {
	t.Helper()
	root := t.TempDir()
	store := storage.NewInMemoryStorage()
	runStore := storage.NewRunStore()
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    singleMockDriverFactory{driver: drv},
		Storage:    store,
		RunStore:   runStore,
		RunStarter: testRunStarter(root),
	})
	return runServer{root: root, store: store, cs: cs, call: func(name string, args map[string]any) map[string]any {
		_, decoded := callTool(t, cs, ctx, name, args)
		return decoded
	}}
}

func (r runServer) onlyMeta(t *testing.T) runfolder.Meta {
	t.Helper()
	metas, _, err := runfolder.NewReader(r.root).List()
	if err != nil || len(metas) != 1 {
		t.Fatalf("run folders = %d (%v), want exactly one", len(metas), err)
	}
	return metas[0]
}

func TestStartRunWritesAnOpenRunFolderLabeledWithTheDevice(t *testing.T) {
	r := newRunServer(t)
	started := r.call("start_run", map[string]any{"name": "smoke", "source": "exploratory"})
	id, _ := started["id"].(string)
	if started["ok"] != true {
		t.Fatalf("start_run = %v", started)
	}
	if !strings.Contains(id, "-mcp-") {
		t.Fatalf("id %q, want the neutral label before any device is selected", id)
	}
	meta := r.onlyMeta(t)
	if meta.RunID != id || meta.Kind != runfolder.KindMCP || meta.CompletedAt != nil {
		t.Fatalf("open meta = %+v", meta)
	}
	if meta.MCP == nil || meta.MCP.Name != "smoke" || meta.MCP.Source != "exploratory" {
		t.Fatalf("mcp block = %+v", meta.MCP)
	}
	if !runfolder.NewLockLiveness(r.root).IsLive(id) {
		t.Fatal("an open run does not hold its lock")
	}
}

func TestStartRunLabelsTheFolderWithTheSelectedDevice(t *testing.T) {
	r := newRunServer(t)
	selectMockDevice(t, r.cs, t.Context())
	started := r.call("start_run", map[string]any{"name": "smoke"})

	id, _ := started["id"].(string)
	if !strings.Contains(id, "-emulator-5554-") {
		t.Fatalf("id %q, want the device id as the label", id)
	}
	meta := r.onlyMeta(t)
	if meta.DeviceID != "emulator-5554" || meta.Device == nil || meta.Device.Platform != "android" {
		t.Fatalf("device in meta = %+v %+v", meta.DeviceID, meta.Device)
	}
}

func TestFinishRunFinalizesTheFolderWithTheOutcomeOfTheStatus(t *testing.T) {
	cases := []struct {
		status  any
		outcome runfolder.Outcome
		result  runfolder.Result
	}{
		{nil, runfolder.OutcomePassed, runfolder.ResultPassed},
		{"passed", runfolder.OutcomePassed, runfolder.ResultPassed},
		{"failed", runfolder.OutcomeFailed, runfolder.ResultFailed},
		{"error", runfolder.OutcomeError, runfolder.ResultFailed},
	}
	for _, c := range cases {
		r := newRunServer(t)
		r.call("start_run", map[string]any{"name": "smoke"})
		r.call("report_bug", map[string]any{"title": "t", "description": "d", "captureScreenshot": false})
		args := map[string]any{"summary": "done"}
		if c.status != nil {
			args["status"] = c.status
		}
		finished := r.call("finish_run", args)
		if finished["ok"] != true {
			t.Fatalf("finish_run(%v) = %v", c.status, finished)
		}
		meta := r.onlyMeta(t)
		if meta.Outcome != c.outcome || meta.Result != c.result || meta.CompletedAt == nil {
			t.Fatalf("status %v: meta = %s/%s completed=%v, want %s/%s", c.status, meta.Result, meta.Outcome, meta.CompletedAt, c.result, c.outcome)
		}
		if meta.MCP.Summary == nil || *meta.MCP.Summary != "done" || len(meta.MCP.Findings) != 1 {
			t.Fatalf("mcp block = %+v", meta.MCP)
		}
		if runfolder.NewLockLiveness(r.root).IsLive(meta.RunID) {
			t.Fatal("a finished run still holds its lock")
		}
		if keys, err := r.store.List(storage.RunKeyPrefix); err != nil || len(keys) != 0 {
			t.Fatalf("flat run records = %v (%v), want none", keys, err)
		}
	}
}

func TestFinishRunWithNoOpenRunReportsItAndWritesNothing(t *testing.T) {
	r := newRunServer(t)
	if got := r.call("finish_run", map[string]any{}); got["ok"] != false {
		t.Fatalf("finish_run = %v, want ok:false", got)
	}
	if metas, _, _ := runfolder.NewReader(r.root).List(); len(metas) != 0 {
		t.Fatalf("run folders = %d, want none", len(metas))
	}
}

func TestStartRunFinalizesAnOpenPredecessorAsAnError(t *testing.T) {
	r := newRunServer(t)
	first := r.call("start_run", map[string]any{"name": "first"})["id"].(string)
	second := r.call("start_run", map[string]any{"name": "second"})

	if second["erroredPredecessor"] != first {
		t.Fatalf("start_run = %v, want erroredPredecessor %s", second, first)
	}
	meta, ok, err := runfolder.NewReader(r.root).Get(first)
	if err != nil || !ok || meta.Outcome != runfolder.OutcomeError || meta.Result != runfolder.ResultFailed {
		t.Fatalf("predecessor = %+v ok=%v err=%v", meta, ok, err)
	}
	if meta.MCP.Summary == nil || *meta.MCP.Summary != forceClosedPredecessorSummary {
		t.Fatalf("predecessor summary = %v", meta.MCP.Summary)
	}
	if runfolder.NewLockLiveness(r.root).IsLive(first) {
		t.Fatal("the force-closed run still holds its lock")
	}
}

func TestUpdateRunSummaryWritesIntoTheOpenRunAndSurvivesFinish(t *testing.T) {
	r := newRunServer(t)
	id := r.call("start_run", map[string]any{"name": "smoke"})["id"].(string)

	if got := r.call("update_run_summary", map[string]any{"id": id, "summary": "while open"}); got["ok"] != true {
		t.Fatalf("update_run_summary = %v", got)
	}
	if meta := r.onlyMeta(t); meta.MCP.Summary == nil || *meta.MCP.Summary != "while open" || meta.CompletedAt != nil {
		t.Fatalf("open meta = %+v", meta.MCP)
	}
	r.call("finish_run", map[string]any{})
	if meta := r.onlyMeta(t); meta.MCP.Summary == nil || *meta.MCP.Summary != "while open" {
		t.Fatalf("summary lost on finish: %+v", meta.MCP)
	}
	if got := r.call("update_run_summary", map[string]any{"id": id, "summary": "after"}); got["ok"] != true {
		t.Fatalf("update after finish = %v", got)
	}
	if meta := r.onlyMeta(t); *meta.MCP.Summary != "after" || meta.Outcome != runfolder.OutcomePassed {
		t.Fatalf("finished meta = %+v", meta)
	}
}

func TestARunHeldByAnotherProcessListsAsRunning(t *testing.T) {
	r := newRunServer(t)
	w, err := testRunStarter(r.root).Start("cli", runfolder.Meta{Kind: runfolder.KindScript, ScriptName: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	got := r.call("get_run", map[string]any{"id": w.ID()})["run"].(map[string]any)
	if got["status"] != "running" || got["outcome"] != nil {
		t.Fatalf("held run = %v, want running with no outcome", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got = r.call("get_run", map[string]any{"id": w.ID()})["run"].(map[string]any)
	if got["outcome"] != "interrupted" {
		t.Fatalf("released run = %v, want interrupted", got)
	}
}

func TestGetRunReadsAFlatRecordAndAnMCPRunFolder(t *testing.T) {
	r := newRunServer(t)
	id := r.call("start_run", map[string]any{"name": "smoke"})["id"].(string)
	r.call("finish_run", map[string]any{})
	finished := int64(9)
	flat := storage.PersistedRun{
		ID: "flat", Name: "old", Status: storage.RunStatusFailed, StartedAt: 1, FinishedAt: &finished,
		Findings: []storage.RunBugReport{},
	}
	if err := r.store.Save(storage.RunKey("flat"), flat); err != nil {
		t.Fatal(err)
	}

	run := r.call("get_run", map[string]any{"id": id})["run"].(map[string]any)
	if run["name"] != "smoke" || run["outcome"] != "passed" || run["result"] != "passed" {
		t.Fatalf("v1 get_run = %v", run)
	}
	run = r.call("get_run", map[string]any{"id": "flat"})["run"].(map[string]any)
	if run["name"] != "old" || run["outcome"] != "failed" {
		t.Fatalf("flat get_run = %v", run)
	}
	if listed := r.call("list_runs", map[string]any{}); listed["totalMatching"] != float64(2) {
		t.Fatalf("list_runs = %v, want both forms", listed)
	}
}

func requireRunBusy(t *testing.T, got map[string]any, id string) {
	t.Helper()
	if got["code"] != "RUN_BUSY" || !strings.Contains(got["message"].(string), id) || got["hint"] == "" {
		t.Fatalf("got %v, want RUN_BUSY naming %s with a remedy", got, id)
	}
}

func TestDeleteRunRefusesTheRunOpenedByStartRun(t *testing.T) {
	r := newRunServer(t)
	id := r.call("start_run", map[string]any{"name": "open"})["id"].(string)

	requireRunBusy(t, r.call("delete_run", map[string]any{"id": id}), id)

	if _, err := os.Stat(runfolder.RunDir(r.root, id)); err != nil {
		t.Fatalf("open run folder was removed: %v", err)
	}
	r.call("finish_run", map[string]any{})
	if got := r.call("delete_run", map[string]any{"id": id}); got["ok"] != true {
		t.Fatalf("delete_run after finish_run = %v", got)
	}
}

func TestRunsHeldByAnotherWriterAreBusyForDeleteAndSummaryUpdate(t *testing.T) {
	r := newRunServer(t)
	w, err := testRunStarter(r.root).Start("cli", runfolder.Meta{Kind: runfolder.KindMCP, MCP: &runfolder.MCPInfo{Name: "other"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	requireRunBusy(t, r.call("delete_run", map[string]any{"id": w.ID()}), w.ID())
	requireRunBusy(t, r.call("update_run_summary", map[string]any{"id": w.ID(), "summary": "x"}), w.ID())
	if _, err := os.Stat(w.Dir()); err != nil {
		t.Fatalf("held run folder was removed: %v", err)
	}
}

func TestFinishRunReportsAFolderThatCannotBeFinalizedAsAToolError(t *testing.T) {
	r := newRunServer(t)
	id := r.call("start_run", map[string]any{"name": "smoke"})["id"].(string)
	if err := os.RemoveAll(runfolder.RunDir(r.root, id)); err != nil {
		t.Fatal(err)
	}

	got := r.call("finish_run", map[string]any{})

	if got["ok"] == true || got["code"] == nil || !strings.Contains(got["message"].(string), id) {
		t.Fatalf("finish_run = %v, want a tool error naming the run", got)
	}
}

func TestNewServerRequiresARunStarterThatNamesItsRoot(t *testing.T) {
	deps := ServerDeps{Probe: device.NewStaticProbe(), Factory: device.NewMockDriverFactory(), Storage: storage.NewInMemoryStorage()}
	if _, err := NewServer(Info{Name: "t", Version: "0"}, deps); err == nil {
		t.Fatal("NewServer accepted deps with no RunStarter")
	}
	deps.RunStarter = testRunStarter("")
	if _, err := NewServer(Info{Name: "t", Version: "0"}, deps); err == nil {
		t.Fatal("NewServer accepted a RunStarter with no root")
	}
}

func TestDeleteAndSummaryUpdateAskToWaitWhileARunMigrationHoldsTheLock(t *testing.T) {
	r := newRunServer(t)
	finished := int64(9)
	flat := storage.PersistedRun{
		ID: "flat", Name: "old", Status: storage.RunStatusFailed, StartedAt: 1, FinishedAt: &finished,
		Findings: []storage.RunBugReport{},
	}
	if err := r.store.Save(storage.RunKey("flat"), flat); err != nil {
		t.Fatal(err)
	}
	lock, err := runfolder.LockMigration(r.root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []map[string]any{
		r.call("delete_run", map[string]any{"id": "flat"}),
		r.call("update_run_summary", map[string]any{"id": "flat", "summary": "x"}),
	} {
		if got["code"] != "RUN_BUSY" || !strings.Contains(got["message"].(string), "migration") || got["hint"] == "" {
			t.Fatalf("got %v, want RUN_BUSY naming the migration with a remedy", got)
		}
	}
	_ = lock.Release()
	if got := r.call("delete_run", map[string]any{"id": "flat"}); got["ok"] != true {
		t.Fatalf("delete after the migration = %v", got)
	}
}
