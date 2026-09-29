package scenario

import (
	"context"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

type noopLogger struct{}

func (noopLogger) Debug(string, logging.LogContext)        {}
func (noopLogger) Info(string, logging.LogContext)         {}
func (noopLogger) Warn(string, logging.LogContext)         {}
func (noopLogger) Error(string, error, logging.LogContext) {}
func (noopLogger) Child(logging.LogContext) logging.Logger { return noopLogger{} }

// recordedLog is one Info call a recordingLogger captured, with its
// own context merged over whatever context the Child chain that
// produced the logger carried.
type recordedLog struct {
	msg string
	ctx logging.LogContext
}

// recordingLogger records every Info call, merging Child's context
// down the chain the way a real logging.Logger implementation does,
// so a test can assert a specific field (for example "runId") reached
// a logged entry through Child rather than being dropped.
type recordingLogger struct {
	ctx  logging.LogContext
	logs *[]recordedLog
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{logs: &[]recordedLog{}}
}

func (r *recordingLogger) Debug(string, logging.LogContext) {}
func (r *recordingLogger) Info(msg string, ctx logging.LogContext) {
	*r.logs = append(*r.logs, recordedLog{msg: msg, ctx: mergeLogContext(r.ctx, ctx)})
}
func (r *recordingLogger) Warn(string, logging.LogContext)         {}
func (r *recordingLogger) Error(string, error, logging.LogContext) {}
func (r *recordingLogger) Child(ctx logging.LogContext) logging.Logger {
	return &recordingLogger{ctx: mergeLogContext(r.ctx, ctx), logs: r.logs}
}

func mergeLogContext(base, overlay logging.LogContext) logging.LogContext {
	merged := make(logging.LogContext, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

// testHierarchy stages one visible element whose text is "ok",
// returned repeatedly once mock.Driver's staged queue is exhausted,
// so it drives any number of child scripts within one test.
var testHierarchy = wire.TreeNodeWire{
	Attributes: map[string]string{},
	Children: []wire.TreeNodeWire{
		{Attributes: map[string]string{"text": "ok"}, Children: []wire.TreeNodeWire{}},
	},
}

const passYaml = "appId: com.test\nname: pass\n---\n- launchApp\n- assertVisible: ok\n"
const failYaml = "appId: com.test\nname: fail\n---\n- launchApp\n- assertVisible: missing\n"

func newFixture(t *testing.T) *orchestra.Orchestra {
	t.Helper()
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(testHierarchy)
	clock := orchestra.NewFakeClock(0)
	return orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock})
}

func TestScenarioRunner_StopOnFailureSkipsRemainder(t *testing.T) {
	orch := newFixture(t)
	def, err := script.ParseScenario("name: stop\nscripts:\n  - pass.yml\n  - fail.yml\n  - pass2.yml\n")
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		LoadScript: MemoryScriptLoader(map[string]string{
			"pass.yml":  passYaml,
			"fail.yml":  failYaml,
			"pass2.yml": passYaml,
		}),
	})

	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Ok {
		t.Errorf("Ok = true, want false")
	}
	if result.PassedScripts != 1 {
		t.Errorf("PassedScripts = %d, want 1", result.PassedScripts)
	}
	if len(result.Scripts) != 3 {
		t.Fatalf("len(Scripts) = %d, want 3", len(result.Scripts))
	}

	if result.Scripts[0].Status != StatusPassed {
		t.Errorf("Scripts[0].Status = %q, want %q", result.Scripts[0].Status, StatusPassed)
	}
	if result.Scripts[1].Status != StatusFailed {
		t.Errorf("Scripts[1].Status = %q, want %q", result.Scripts[1].Status, StatusFailed)
	}

	skipped := result.Scripts[2]
	if skipped.Status != StatusSkipped {
		t.Errorf("Scripts[2].Status = %q, want %q", skipped.Status, StatusSkipped)
	}
	if skipped.Result != nil {
		t.Errorf("Scripts[2].Result = %+v, want nil (skipped script must not be loaded or run)", skipped.Result)
	}
	if skipped.DurationMs != 0 {
		t.Errorf("Scripts[2].DurationMs = %d, want 0", skipped.DurationMs)
	}
}

func TestScenarioRunner_ContinueOnFailureRunsEveryScript(t *testing.T) {
	orch := newFixture(t)
	def, err := script.ParseScenario("name: continue\nonFailure: continue\nscripts:\n  - pass.yml\n  - fail.yml\n  - pass2.yml\n")
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		LoadScript: MemoryScriptLoader(map[string]string{
			"pass.yml":  passYaml,
			"fail.yml":  failYaml,
			"pass2.yml": passYaml,
		}),
	})

	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Ok {
		t.Errorf("Ok = true, want false")
	}
	if result.PassedScripts != 2 {
		t.Errorf("PassedScripts = %d, want 2", result.PassedScripts)
	}

	wantStatuses := []ScriptRunStatus{StatusPassed, StatusFailed, StatusPassed}
	for i, want := range wantStatuses {
		if result.Scripts[i].Status != want {
			t.Errorf("Scripts[%d].Status = %q, want %q", i, result.Scripts[i].Status, want)
		}
		if result.Scripts[i].Result == nil {
			t.Errorf("Scripts[%d].Result = nil, want a ScriptResult (onFailure: continue must still run it)", i)
		}
	}
}

func TestScenarioRunner_EmitsLifecycleEventsInOrder(t *testing.T) {
	orch := newFixture(t)
	def, err := script.ParseScenario("name: events\nscripts:\n  - pass.yml\n  - fail.yml\n  - pass2.yml\n")
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	var events []ScenarioEvent
	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		LoadScript: MemoryScriptLoader(map[string]string{
			"pass.yml":  passYaml,
			"fail.yml":  failYaml,
			"pass2.yml": passYaml,
		}),
		OnScenarioEvent: func(e ScenarioEvent) {
			events = append(events, e)
		},
	})

	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatalf("Ok = true, want false")
	}

	wantTypes := []ScenarioEventType{
		ScenarioStarted,
		ScriptStarted, ScriptCompleted,
		ScriptStarted, ScriptCompleted,
		ScenarioCompleted,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantTypes), events)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Errorf("events[%d].Type = %q, want %q", i, events[i].Type, want)
		}
	}

	// The second script (fail.yml) skips the third (pass2.yml) via the
	// default onFailure: "stop", so no ScriptStarted/ScriptCompleted
	// pair fires for it.
	if events[0].ScenarioName != "events" || events[0].TotalScripts != 3 {
		t.Errorf("scenarioStarted = %+v, want scenarioName=events totalScripts=3", events[0])
	}
	if events[1].ScriptIndex != 0 || events[1].ScriptPath != "pass.yml" {
		t.Errorf("first scriptStarted = %+v, want index=0 path=pass.yml", events[1])
	}
	if events[2].Status != StatusPassed {
		t.Errorf("first scriptCompleted.Status = %q, want %q", events[2].Status, StatusPassed)
	}
	if events[3].ScriptIndex != 1 || events[3].ScriptPath != "fail.yml" {
		t.Errorf("second scriptStarted = %+v, want index=1 path=fail.yml", events[3])
	}
	if events[4].Status != StatusFailed {
		t.Errorf("second scriptCompleted.Status = %q, want %q", events[4].Status, StatusFailed)
	}
	last := events[5]
	if last.Ok || last.TotalScripts != 3 || last.PassedScripts != 1 {
		t.Errorf("scenarioCompleted = %+v, want ok=false totalScripts=3 passedScripts=1", last)
	}
}

// TestScenarioRunner_Run_LaunchesAppOnceAcrossScripts proves a
// scenario is one flow for launch-dedup purposes: two child scripts
// that each declare the same appId and start with launchApp result in
// exactly one driver-level launch, not two.
func TestScenarioRunner_Run_LaunchesAppOnceAcrossScripts(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(testHierarchy)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.NewFakeClock(0)})

	def, err := script.ParseScenario("name: dedup\nscripts:\n  - pass.yml\n  - pass2.yml\n")
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		LoadScript: MemoryScriptLoader(map[string]string{
			"pass.yml":  passYaml,
			"pass2.yml": passYaml,
		}),
	})

	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("Ok = false, want true: %+v", result)
	}

	launches := 0
	for _, call := range d.Calls {
		if call.Method == "LaunchApp" {
			launches++
		}
	}
	if launches != 1 {
		t.Fatalf("got %d driver-level launches across the scenario's two scripts, want exactly 1 (a scenario is one flow)", launches)
	}
}

func TestScenarioRunner_ExternalEnvWinsOverScenarioEnv(t *testing.T) {
	orch := newFixture(t)
	def, err := script.ParseScenario("name: env\nenv:\n  x: \"1\"\nscripts:\n  - env-check.yml\n")
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		LoadScript: MemoryScriptLoader(map[string]string{
			"env-check.yml": "appId: com.test\nname: env check\n---\n- launchApp\n- assertVisible: \"${x}\"\n",
		}),
		ExternalEnv: map[string]string{"x": "ok"},
	})

	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.Ok {
		t.Fatalf("Ok = false, want true (externalEnv %q should have overridden the scenario's own env %q)", "ok", "1")
	}
}

// TestScenarioRunner_Run_EmitsStepTraceAndOutcomeLogCarryingRunID
// confirms a scenario run's RunID reaches both every child script's
// own "script.step" trace (via the fresh runner.ScriptRunner each
// child gets) and a "scenario.run" outcome entry logged once the
// scenario itself completes.
func TestScenarioRunner_Run_EmitsStepTraceAndOutcomeLogCarryingRunID(t *testing.T) {
	orch := newFixture(t)
	def, err := script.ParseScenario("name: traced\nscripts:\n  - pass.yml\n")
	if err != nil {
		t.Fatalf("ParseScenario: %v", err)
	}

	logger := newRecordingLogger()
	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    logger,
		RunID:     "run-scenario-1",
		Commands:  runner.DefaultCommands(),
		LoadScript: MemoryScriptLoader(map[string]string{
			"pass.yml": passYaml,
		}),
	})

	result, err := r.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("Ok = false, want true")
	}

	var stepLogs, outcomeLogs int
	for _, entry := range *logger.logs {
		if entry.ctx["runId"] != "run-scenario-1" {
			t.Fatalf("got runId=%v on %q, want %q on every log entry", entry.ctx["runId"], entry.msg, "run-scenario-1")
		}
		switch entry.msg {
		case "script.step":
			stepLogs++
		case "scenario.run":
			outcomeLogs++
			if entry.ctx["name"] != "traced" {
				t.Errorf("scenario.run name=%v, want %q", entry.ctx["name"], "traced")
			}
			if entry.ctx["ok"] != true {
				t.Errorf("scenario.run ok=%v, want true", entry.ctx["ok"])
			}
		}
	}
	if stepLogs == 0 {
		t.Errorf("got no script.step log entries from the child script, want at least one")
	}
	if outcomeLogs != 1 {
		t.Errorf("got %d scenario.run log entries, want exactly 1", outcomeLogs)
	}
}
