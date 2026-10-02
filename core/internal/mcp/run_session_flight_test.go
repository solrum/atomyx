package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/storage"
)

func newFlightSession(t *testing.T) (*runSession, string) {
	t.Helper()
	root := t.TempDir()
	return newRunSession(runSessionDeps{
		Store:   storage.NewRunStore(),
		Catalog: testRunCatalog(storage.NewInMemoryStorage(), root),
		Starter: testRunStarter(root),
		Root:    root,
		Clock:   orchestra.NewFakeClock(0),
		Logger:  logging.NoopLogger{},
	}), root
}

// scriptProbe is a script execution the test steers. started closes
// when the script is running, and sawCancel closes when its target
// context ends. The script returns result once release closes, and a
// script made with ignoreCancel does not watch its context at all.
type scriptProbe struct {
	started   chan struct{}
	sawCancel chan struct{}
	release   chan struct{}
	result    runner.ScriptResult
}

func newScriptProbe(result runner.ScriptResult) *scriptProbe {
	return &scriptProbe{
		started: make(chan struct{}), sawCancel: make(chan struct{}), release: make(chan struct{}), result: result,
	}
}

func (p *scriptProbe) exec(watchCancel bool) func(scriptTarget) (runner.ScriptResult, error) {
	return func(target scriptTarget) (runner.ScriptResult, error) {
		close(p.started)
		if watchCancel {
			<-target.Ctx.Done()
			close(p.sawCancel)
		}
		<-p.release
		return p.result, nil
	}
}

type scriptOutput struct {
	result runner.ScriptResult
	err    error
}

// runScript starts the probe's script in session and returns once it is
// executing. The output arrives on the returned channel when it returns.
func runScript(t *testing.T, session *runSession, name string, p *scriptProbe, watchCancel bool) <-chan scriptOutput {
	t.Helper()
	out := make(chan scriptOutput, 1)
	go func() {
		result, err := session.RunScript(context.Background(), name, nil, p.exec(watchCancel))
		out <- scriptOutput{result, err}
	}()
	<-p.started
	return out
}

func readMeta(t *testing.T, root, id string) runfolder.Meta {
	t.Helper()
	meta, ok, err := runfolder.NewReader(root).Get(id)
	if err != nil || !ok {
		t.Fatalf("reading run %s: ok=%v err=%v", id, ok, err)
	}
	return meta
}

func intPtr(n int) *int { return &n }

func TestFinishLeavesTheRunOpenWhenAScriptDoesNotReturnBeforeTheCallerGivesUp(t *testing.T) {
	session, root := newFlightSession(t)
	started, err := session.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := started.Run.ID
	p := newScriptProbe(runner.ScriptResult{})
	out := runScript(t, session, "stuck", p, false)
	gaveUp, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := session.Finish(gaveUp, "passed", nil); err == nil || !strings.Contains(err.Error(), "script") {
		t.Fatalf("Finish with a script still running = %v, want an error naming the script", err)
	}

	if session.OpenRunID() != id {
		t.Fatal("the run was closed while a script was still executing")
	}
	if meta := readMeta(t, root, id); meta.CompletedAt != nil {
		t.Fatalf("the run was finalized under a running script: %+v", meta)
	}
	close(p.release)
	<-out
	finished, err := session.Finish(context.Background(), "passed", nil)
	if err != nil || finished.Run == nil {
		t.Fatalf("Finish after the script returned = %+v, %v", finished, err)
	}
}

func TestFinishCancelsARunningScriptAndFinalizesOnlyAfterItReturns(t *testing.T) {
	session, root := newFlightSession(t)
	started, err := session.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := started.Run.ID
	p := newScriptProbe(runner.ScriptResult{TotalSteps: 7, FailedAtStep: intPtr(3)})
	out := runScript(t, session, "login", p, true)
	finished := make(chan error, 1)
	go func() {
		_, err := session.Finish(context.Background(), "failed", nil)
		finished <- err
	}()

	<-p.sawCancel
	if meta := readMeta(t, root, id); meta.CompletedAt != nil {
		t.Fatalf("the run was finalized before its script returned: %+v", meta)
	}
	close(p.release)
	<-out
	if err := <-finished; err != nil {
		t.Fatal(err)
	}

	meta := readMeta(t, root, id)
	if meta.Outcome != runfolder.OutcomeFailed || meta.TotalSteps != 7 || meta.FailedAtStep == nil || *meta.FailedAtStep != 3 {
		t.Fatalf("meta = %+v, want the finished run to carry the totals of the script that ended before it", meta)
	}
}

func TestStartCancelsTheScriptsOfTheRunItSupersedes(t *testing.T) {
	session, root := newFlightSession(t)
	first, err := session.Start(context.Background(), "first", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := newScriptProbe(runner.ScriptResult{TotalSteps: 2})
	out := runScript(t, session, "old", p, true)
	second := make(chan startedRun, 1)
	go func() {
		started, err := session.Start(context.Background(), "second", nil, nil)
		if err != nil {
			t.Error(err)
		}
		second <- started
	}()

	<-p.sawCancel
	if meta := readMeta(t, root, first.Run.ID); meta.CompletedAt != nil {
		t.Fatalf("the superseded run was finalized before its script returned: %+v", meta)
	}
	close(p.release)
	<-out
	started := <-second

	meta := readMeta(t, root, first.Run.ID)
	if meta.Outcome != runfolder.OutcomeError || meta.TotalSteps != 2 {
		t.Fatalf("superseded meta = %+v, want outcome error with the script totals", meta)
	}
	if started.Superseded == nil || session.OpenRunID() != started.Run.ID {
		t.Fatal("the new run is not the open run")
	}
}

func TestTheLastScriptToEndSetsTheRunTotals(t *testing.T) {
	session, root := newFlightSession(t)
	started, err := session.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := started.Run.ID
	for _, result := range []runner.ScriptResult{
		{TotalSteps: 5, FailedAtStep: intPtr(4)},
		{TotalSteps: 2, Ok: true},
	} {
		if _, err := session.RunScript(context.Background(), "s", nil, func(scriptTarget) (runner.ScriptResult, error) {
			return result, nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	meta := readMeta(t, root, id)
	if meta.CompletedAt != nil || meta.TotalSteps != 2 || meta.FailedAtStep != nil {
		t.Fatalf("open meta = %+v, want the totals of the second script and no failed step", meta)
	}
}

func TestFinishReportsARunFolderThatCannotBeFinalized(t *testing.T) {
	session, root := newFlightSession(t)
	started, err := session.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(runfolder.RunDir(root, started.Run.ID)); err != nil {
		t.Fatal(err)
	}

	finished, err := session.Finish(context.Background(), "passed", nil)

	if err == nil || !strings.Contains(err.Error(), started.Run.ID) {
		t.Fatalf("Finish = %v, want an error naming the run", err)
	}
	if finished.Run == nil || session.OpenRunID() != "" {
		t.Fatal("the in-memory run must be closed even though its folder was not finalized")
	}
}

func TestCloseOpenRunKeepsASummaryTheAgentSetAndFillsAnEmptyOne(t *testing.T) {
	for name, tc := range map[string]struct{ agent, want string }{
		"agent summary kept":  {agent: "agent notes", want: "agent notes"},
		"empty summary taken": {agent: "", want: "closed by the test"},
	} {
		t.Run(name, func(t *testing.T) {
			session, root := newFlightSession(t)
			started, err := session.Start(context.Background(), "smoke", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.agent != "" {
				if ok, err := session.SetSummary(started.Run.ID, tc.agent); !ok || err != nil {
					t.Fatalf("SetSummary: %v %v", ok, err)
				}
			}

			session.CloseOpenRun(runfolder.OutcomeCancelled, "closed by the test")

			meta := readMeta(t, root, started.Run.ID)
			if meta.MCP == nil || meta.MCP.Summary == nil || *meta.MCP.Summary != tc.want {
				t.Fatalf("summary = %+v, want %q", meta.MCP, tc.want)
			}
		})
	}
}

func TestCloseFinalizesTheOpenRunAsCancelledWithItsFindings(t *testing.T) {
	session, root := newFlightSession(t)
	started, err := session.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.store.RecordBug(storage.RecordBugOptions{Title: "crash", Description: "d"}); err != nil {
		t.Fatal(err)
	}

	session.Close()

	meta := readMeta(t, root, started.Run.ID)
	if meta.Outcome != runfolder.OutcomeCancelled || meta.Result != runfolder.ResultCancelled || meta.CompletedAt == nil {
		t.Fatalf("meta = %+v, want a cancelled run", meta)
	}
	if meta.MCP == nil || len(meta.MCP.Findings) != 1 || meta.MCP.Summary == nil || *meta.MCP.Summary != shutdownSummary {
		t.Fatalf("mcp block = %+v, want the finding and the shutdown summary", meta.MCP)
	}
	if runfolder.NewLockLiveness(root).IsLive(started.Run.ID) {
		t.Fatal("the closed run still holds its lock")
	}
	session.Close()
}

func TestCloseCancelsAScriptThatIsItsOwnRunAndLetsItFinalizeAsCancelled(t *testing.T) {
	session, root := newFlightSession(t)
	p := newScriptProbe(runner.ScriptResult{TotalSteps: 4})
	out := runScript(t, session, "bare", p, true)
	closed := make(chan struct{})
	go func() {
		session.Close()
		close(closed)
	}()

	<-p.sawCancel
	close(p.release)
	<-out
	<-closed

	metas, _, err := runfolder.NewReader(root).List()
	if err != nil || len(metas) != 1 {
		t.Fatalf("run folders = %d (%v)", len(metas), err)
	}
	if metas[0].Outcome != runfolder.OutcomeCancelled || metas[0].CompletedAt == nil {
		t.Fatalf("bare script run = %+v, want cancelled", metas[0])
	}
}

func TestACloseTheServerStartsFinalizesAfterTheDrainTimeoutWhenAScriptIgnoresCancellation(t *testing.T) {
	session, root := newFlightSession(t)
	session.drainTimeout = time.Millisecond
	started, err := session.Start(context.Background(), "smoke", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := newScriptProbe(runner.ScriptResult{})
	out := runScript(t, session, "stuck", p, false)

	id := session.CloseOpenRun(runfolder.OutcomeDeviceLost, "lost")

	if id != started.Run.ID {
		t.Fatalf("closed %q, want %q", id, started.Run.ID)
	}
	if meta := readMeta(t, root, id); meta.Outcome != runfolder.OutcomeDeviceLost {
		t.Fatalf("meta = %+v, want deviceLost", meta)
	}
	close(p.release)
	<-out
}
