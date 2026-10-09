package scenario

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
)

func TestSuiteRunner_FailingEntryDoesNotStopTheRest(t *testing.T) {
	orch := newFixture(t)
	entries := []SuiteEntry{
		{Path: "scripts/a.yml", Content: passYaml},
		{Path: "scripts/b.yml", Content: failYaml},
		{Path: "scripts/c.yml", Content: passYaml},
	}

	r := NewSuiteRunner(SuiteRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
	})

	result, err := r.Run(context.Background(), entries, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Ok {
		t.Errorf("Ok = true, want false")
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if result.Passed != 2 {
		t.Errorf("Passed = %d, want 2", result.Passed)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("len(Entries) = %d, want 3 (every entry must run regardless of an earlier failure)", len(result.Entries))
	}

	wantStatuses := []SuiteEntryStatus{EntryPassed, EntryFailed, EntryPassed}
	for i, want := range wantStatuses {
		if result.Entries[i].Status != want {
			t.Errorf("Entries[%d].Status = %q, want %q", i, result.Entries[i].Status, want)
		}
	}
}

// TestSuiteRunner_Run_ResetsLaunchedAppsPerEntry proves each suite
// entry is an independent test case, unlike a scenario's own scripts:
// two entries that each declare the same appId and start with
// launchApp result in exactly two driver-level launches, one per
// entry, because the suite resets launch tracking between entries.
func TestSuiteRunner_Run_ResetsLaunchedAppsPerEntry(t *testing.T) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(testHierarchy)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.NewFakeClock(0)})

	entries := []SuiteEntry{
		{Path: "scripts/a.yml", Content: passYaml},
		{Path: "scripts/b.yml", Content: passYaml},
	}

	r := NewSuiteRunner(SuiteRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
	})

	result, err := r.Run(context.Background(), entries, nil)
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
	if launches != 2 {
		t.Fatalf("got %d driver-level launches across the suite's two independent entries, want exactly 2 (each entry is its own test case)", launches)
	}
}

// TestSuiteRunner_Run_EmitsStepTraceAndOutcomeLogCarryingRunID
// confirms a suite run's RunID reaches every entry's own "script.step"
// trace, whether the entry is a plain script or a scenario (whose
// nested child script gets its own runner.ScriptRunner in turn), the
// nested scenario's own "scenario.run" outcome entry, and the suite's
// own "suite.run" outcome entry logged once the whole suite completes.
func TestSuiteRunner_Run_EmitsStepTraceAndOutcomeLogCarryingRunID(t *testing.T) {
	orch := newFixture(t)
	entries := []SuiteEntry{
		{Path: "scripts/a.yml", Content: passYaml},
		{Path: "scripts/nested.yml", Content: "name: nested\nscripts:\n  - pass.yml\n"},
	}

	logger := newRecordingLogger()
	r := NewSuiteRunner(SuiteRunnerDeps{
		Orchestra: orch,
		Clock:     orchestra.NewFakeClock(0),
		Logger:    logger,
		RunID:     "run-suite-1",
		Commands:  runner.DefaultCommands(),
		CreateLoadScript: func(string) LoadScript {
			return MemoryScriptLoader(map[string]string{"pass.yml": passYaml})
		},
	})

	result, err := r.Run(context.Background(), entries, &SuiteRunOptions{SuiteName: strPtr("smoke")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("Ok = false, want true")
	}

	var stepLogs, scenarioLogs, suiteLogs int
	for _, entry := range *logger.logs {
		if entry.ctx["runId"] != "run-suite-1" {
			t.Fatalf("got runId=%v on %q, want %q on every log entry", entry.ctx["runId"], entry.msg, "run-suite-1")
		}
		switch entry.msg {
		case "script.step":
			stepLogs++
		case "scenario.run":
			scenarioLogs++
		case "suite.run":
			suiteLogs++
			if entry.ctx["name"] != "smoke" {
				t.Errorf("suite.run name=%v, want %q", entry.ctx["name"], "smoke")
			}
			if entry.ctx["ok"] != true {
				t.Errorf("suite.run ok=%v, want true", entry.ctx["ok"])
			}
		}
	}
	if stepLogs == 0 {
		t.Errorf("got no script.step log entries across both entries, want at least one")
	}
	if scenarioLogs != 1 {
		t.Errorf("got %d scenario.run log entries, want exactly 1 (from the nested scenario entry)", scenarioLogs)
	}
	if suiteLogs != 1 {
		t.Errorf("got %d suite.run log entries, want exactly 1", suiteLogs)
	}
}

func strPtr(s string) *string { return &s }

func TestSuiteRunner_Run_CallsEntryHookAroundEveryEntryInOrder(t *testing.T) {
	var events []string
	entries := []SuiteEntry{
		{Path: "scripts/a.yml", Content: passYaml},
		{Path: "scripts/b.yml", Content: failYaml},
		{Path: "scripts/c.yml", Content: "not: [valid"},
	}
	r := NewSuiteRunner(SuiteRunnerDeps{
		Orchestra: newFixture(t),
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		OnEntryStart: func(_ context.Context, target EntryTarget) EntryEnd {
			events = append(events, fmt.Sprintf("start %d %s", target.EntryIndex, target.EntryPath))
			return func(res SuiteEntryResult) []string {
				events = append(events, fmt.Sprintf("end %s %s", res.Path, res.Status))
				return nil
			}
		},
	})

	if _, err := r.Run(context.Background(), entries, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{
		"start 0 scripts/a.yml", "end scripts/a.yml passed",
		"start 1 scripts/b.yml", "end scripts/b.yml failed",
		"start 2 scripts/c.yml", "end scripts/c.yml failed",
	}
	if strings.Join(events, "|") != strings.Join(want, "|") {
		t.Errorf("events = %v, want %v", events, want)
	}
}

func TestSuiteRunner_Run_AcceptsAHookWithNoEndFunction(t *testing.T) {
	starts := 0
	r := NewSuiteRunner(SuiteRunnerDeps{
		Orchestra: newFixture(t),
		Clock:     orchestra.NewFakeClock(0),
		Logger:    noopLogger{},
		Commands:  runner.DefaultCommands(),
		OnEntryStart: func(context.Context, EntryTarget) EntryEnd {
			starts++
			return nil
		},
	})
	if _, err := r.Run(context.Background(), []SuiteEntry{{Path: "a.yml", Content: passYaml}}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if starts != 1 {
		t.Errorf("starts = %d, want 1", starts)
	}
}
