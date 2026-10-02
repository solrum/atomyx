package mcp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/storage"
)

func TestRunScriptStepFailureReportsFailedAtStep(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	script := "appId: com.example.app\nname: Smoke\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
	if result.IsError {
		t.Fatalf("run_script (step failure): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("run_script (step failure): got %v, want ok:false", decoded)
	}
	if decoded["failedAtStep"] != float64(0) {
		t.Fatalf("run_script (step failure) failedAtStep: got %v, want 0", decoded["failedAtStep"])
	}
	steps, ok := decoded["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("run_script (step failure) steps: got %v, want a single entry", decoded["steps"])
	}
	step0 := steps[0].(map[string]any)
	if step0["ok"] != false {
		t.Fatalf("run_script (step failure) steps[0]: got %v, want ok:false", step0)
	}
	if _, present := step0["deviceLost"]; present {
		t.Fatalf("run_script (step failure) steps[0]: got deviceLost present as %v, want omitted for an ordinary failure", step0["deviceLost"])
	}
	if _, present := decoded["deviceLost"]; present {
		t.Fatalf("run_script (step failure): got top-level deviceLost present as %v, want omitted for an ordinary failure", decoded["deviceLost"])
	}
}

// unreachableAgentError models a driver liveness failure the same way
// a real driver's coded error does, so the liveness probe run_script
// triggers after a failed step classifies it as the device having
// gone away rather than an ordinary command failure.
type unreachableAgentError struct{}

func (unreachableAgentError) Error() string  { return "could not reach the agent on the device" }
func (unreachableAgentError) Code() string   { return "AGENT_UNREACHABLE" }
func (unreachableAgentError) Remedy() string { return "Confirm the agent is running, then try again." }

func TestRunScriptDeviceLostFlagsFailedStepAndTopLevel(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	drv.FailNextPingForContract(unreachableAgentError{})
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	script := "appId: com.example.app\nname: Smoke\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
	if result.IsError {
		t.Fatalf("run_script (device lost): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("run_script (device lost): got %v, want ok:false", decoded)
	}
	if decoded["deviceLost"] != true {
		t.Fatalf("run_script (device lost): got deviceLost=%v, want true", decoded["deviceLost"])
	}
	steps, ok := decoded["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("run_script (device lost) steps: got %v, want a single entry", decoded["steps"])
	}
	step0 := steps[0].(map[string]any)
	if step0["deviceLost"] != true {
		t.Fatalf("run_script (device lost) steps[0]: got deviceLost=%v, want true", step0["deviceLost"])
	}
}

const passingScreenshotScript = "appId: com.example.app\nname: Smoke Flow\nsteps:\n  - screenshot: home\n"

func artifactFiles(t *testing.T, runDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(runfolder.ArtifactsDir(runDir))
	if err != nil {
		t.Fatalf("reading artifacts: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestRunScriptInsideAnOpenRunWritesIntoItAndCreatesNoSecondRun(t *testing.T) {
	r := newRunServer(t)
	selectMockDevice(t, r.cs, t.Context())
	id := r.call("start_run", map[string]any{"name": "explore"})["id"].(string)

	for range 2 {
		got := r.call("run_script", map[string]any{"script": passingScreenshotScript})
		if got["ok"] != true {
			t.Fatalf("run_script = %v", got)
		}
	}

	metas, _, err := runfolder.NewReader(r.root).List()
	if err != nil || len(metas) != 1 || metas[0].RunID != id || metas[0].CompletedAt != nil {
		t.Fatalf("run folders = %+v (%v), want only the open run, still open", metas, err)
	}
	files := artifactFiles(t, runfolder.RunDir(r.root, id))
	if len(files) != 2 || files[0] == files[1] {
		t.Fatalf("artifacts = %v, want two distinct screenshots", files)
	}
}

func TestBareRunScriptIsItsOwnRunFinalizedWhenTheScriptEnds(t *testing.T) {
	r := newRunServer(t)
	selectMockDevice(t, r.cs, t.Context())

	if got := r.call("run_script", map[string]any{"script": passingScreenshotScript}); got["ok"] != true {
		t.Fatalf("run_script = %v", got)
	}
	meta := r.onlyMeta(t)
	if meta.Kind != runfolder.KindMCP || meta.Outcome != runfolder.OutcomePassed || meta.CompletedAt == nil {
		t.Fatalf("meta = %+v, want a finalized passed mcp run", meta)
	}
	if !strings.Contains(meta.RunID, "-smoke-flow-") || meta.ScriptName != "Smoke Flow" || meta.TotalSteps != 1 {
		t.Fatalf("meta = %+v, want the script name in the label, name, and step count", meta)
	}
	if len(artifactFiles(t, runfolder.RunDir(r.root, meta.RunID))) != 1 {
		t.Fatal("the script screenshot is not in the run's artifacts")
	}
	if runfolder.NewLockLiveness(r.root).IsLive(meta.RunID) {
		t.Fatal("the finished script run still holds its lock")
	}
}

func TestBareRunScriptThatFailsAStepFinalizesAsFailed(t *testing.T) {
	r := newRunServer(t)
	selectMockDevice(t, r.cs, t.Context())

	script := "appId: com.example.app\nname: Broken\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"
	r.call("run_script", map[string]any{"script": script})

	meta := r.onlyMeta(t)
	if meta.Outcome != runfolder.OutcomeFailed || meta.FailedAtStep == nil || *meta.FailedAtStep != 0 {
		t.Fatalf("meta = %+v, want failed at step 0", meta)
	}
}

func TestScriptOutcomeMapsEveryWayAScriptCanEnd(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	lost := runner.ScriptResult{Abort: &runner.RunAbort{Reason: runner.RunAbortDeviceLost}}
	cases := []struct {
		name   string
		ctx    context.Context
		result runner.ScriptResult
		err    error
		want   runfolder.Outcome
	}{
		{"passed", context.Background(), runner.ScriptResult{Ok: true}, nil, runfolder.OutcomePassed},
		{"failed", context.Background(), runner.ScriptResult{}, nil, runfolder.OutcomeFailed},
		{"device lost", context.Background(), lost, nil, runfolder.OutcomeDeviceLost},
		{"error", context.Background(), runner.ScriptResult{}, errors.New("boom"), runfolder.OutcomeError},
		{"canceled", canceled, runner.ScriptResult{}, nil, runfolder.OutcomeCancelled},
		{"canceled with error", canceled, lost, errors.New("boom"), runfolder.OutcomeCancelled},
		{"passed before cancel", canceled, runner.ScriptResult{Ok: true}, nil, runfolder.OutcomePassed},
	}
	for _, c := range cases {
		if got := scriptOutcome(c.ctx, c.result, c.err); got != c.want {
			t.Errorf("%s: outcome = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestBareScriptRunFinalizesAsCancelledWhenTheContextIsCanceled(t *testing.T) {
	root := t.TempDir()
	runStore := storage.NewRunStore()
	session := newRunSession(runSessionDeps{
		Store: runStore, Catalog: testRunCatalog(storage.NewInMemoryStorage(), root),
		Starter: testRunStarter(root), Clock: orchestra.NewFakeClock(0), Logger: logging.NoopLogger{},
	})
	ctx, cancel := context.WithCancel(context.Background())

	_, err := session.RunScript(ctx, "slow", nil, func(scriptTarget) (runner.ScriptResult, error) {
		cancel()
		return runner.ScriptResult{}, ctx.Err()
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation passed through", err)
	}
	metas, _, readErr := runfolder.NewReader(root).List()
	if readErr != nil || len(metas) != 1 {
		t.Fatalf("run folders = %d (%v)", len(metas), readErr)
	}
	if metas[0].Outcome != runfolder.OutcomeCancelled || metas[0].Result != runfolder.ResultCancelled {
		t.Fatalf("meta = %s/%s, want cancelled", metas[0].Result, metas[0].Outcome)
	}
	if runfolder.NewLockLiveness(root).IsLive(metas[0].RunID) {
		t.Fatal("the canceled run still holds its lock")
	}
}

func TestRunScriptFailuresInOneOpenRunWriteDistinctBundles(t *testing.T) {
	root := t.TempDir()
	drv := mockdriver.New()
	drv.ScreenshotData = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1}
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    singleMockDriverFactory{driver: drv},
		Storage:    storage.NewInMemoryStorage(),
		RunStore:   storage.NewRunStore(),
		RunStarter: testRunStarter(root),
	})
	selectMockDevice(t, cs, ctx)
	started := func() string {
		_, decoded := callTool(t, cs, ctx, "start_run", map[string]any{"name": "two", "source": "regression"})
		id, _ := decoded["id"].(string)
		return id
	}()

	script := "appId: com.example.app\nname: Smoke\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"
	for range 2 {
		_, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
		if decoded["ok"] != false {
			t.Fatalf("run_script = %v, want a failure", decoded)
		}
	}

	artifacts := runfolder.ArtifactsDir(runfolder.RunDir(root, started))
	entries, err := os.ReadDir(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{
		"script-001-step-001-failure.png", "script-001-step-001-log.jsonl", "script-001-step-001-tree.json",
		"script-002-step-001-failure.png", "script-002-step-001-log.jsonl", "script-002-step-001-tree.json",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("artifacts = %v, want %v", names, want)
	}
}
