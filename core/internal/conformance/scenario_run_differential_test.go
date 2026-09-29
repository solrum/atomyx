package conformance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/script"
)

// scriptResultComparable is a runner.ScriptResult normalized for
// comparison against a frozen golden fixture, field for field. It
// reuses runnerComparableStep and runnerComparableShot (defined in
// runner_differential_test.go) so a step or a screenshot compares the
// same way here as it does for the single-script differential.
type scriptResultComparable struct {
	ScriptName   string                 `json:"scriptName"`
	Ok           bool                   `json:"ok"`
	Steps        []runnerComparableStep `json:"steps"`
	TotalSteps   int                    `json:"totalSteps"`
	PassedSteps  int                    `json:"passedSteps"`
	FailedAtStep *int                   `json:"failedAtStep"`
	DurationMs   int64                  `json:"durationMs"`
	Screenshots  []runnerComparableShot `json:"screenshots"`
}

func goScriptResultComparable(r runner.ScriptResult) scriptResultComparable {
	steps := make([]runnerComparableStep, len(r.Steps))
	for i, s := range r.Steps {
		steps[i] = runnerComparableStep{
			StepIndex: s.StepIndex, Command: s.Command, Ok: s.Ok, Detail: s.Detail, DurationMs: s.DurationMs,
		}
	}
	shots := make([]runnerComparableShot, 0, len(r.Artifacts.Screenshots()))
	for _, s := range r.Artifacts.Screenshots() {
		shots = append(shots, runnerComparableShot{Label: s.Label, DataLength: len(s.Data)})
	}
	return scriptResultComparable{
		ScriptName: r.ScriptName, Ok: r.Ok, Steps: steps, TotalSteps: r.TotalSteps,
		PassedSteps: r.PassedSteps, FailedAtStep: r.FailedAtStep, DurationMs: r.DurationMs, Screenshots: shots,
	}
}

type scenarioScriptResultComparable struct {
	ScriptIndex int                     `json:"scriptIndex"`
	ScriptPath  string                  `json:"scriptPath"`
	Status      string                  `json:"status"`
	Result      *scriptResultComparable `json:"result,omitempty"`
	Error       *string                 `json:"error,omitempty"`
	DurationMs  int64                   `json:"durationMs"`
}

type scenarioResultComparable struct {
	ScenarioName  string                           `json:"scenarioName"`
	Ok            bool                             `json:"ok"`
	Scripts       []scenarioScriptResultComparable `json:"scripts"`
	TotalScripts  int                              `json:"totalScripts"`
	PassedScripts int                              `json:"passedScripts"`
	DurationMs    int64                            `json:"durationMs"`
}

func goScenarioResultComparable(r scenario.ScenarioResult) scenarioResultComparable {
	scripts := make([]scenarioScriptResultComparable, len(r.Scripts))
	for i, s := range r.Scripts {
		var res *scriptResultComparable
		if s.Result != nil {
			c := goScriptResultComparable(*s.Result)
			res = &c
		}
		scripts[i] = scenarioScriptResultComparable{
			ScriptIndex: s.ScriptIndex, ScriptPath: s.ScriptPath, Status: string(s.Status),
			Result: res, Error: s.Error, DurationMs: s.DurationMs,
		}
	}
	return scenarioResultComparable{
		ScenarioName: r.ScenarioName, Ok: r.Ok, Scripts: scripts,
		TotalScripts: r.TotalScripts, PassedScripts: r.PassedScripts, DurationMs: r.DurationMs,
	}
}

// newScenarioRunFixture stages a mock driver with scenarioRunHierarchy
// and returns an Orchestra plus the fake clock backing it, shared by
// every scenario-run and suite-run differential case: the corpus's
// single static tree is sticky (mock.Driver repeats the last-staged
// tree once its queue is exhausted), so one fixture serves any number
// of child scripts within one case.
func newScenarioRunFixture() (*orchestra.Orchestra, *orchestra.FakeClock) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	d.StageHierarchy(scenarioRunHierarchy)
	clock := orchestra.NewFakeClock(0)
	return orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: clock}), clock
}

// runGoScenarioCase parses and runs c through the Go scenario runner
// against a fresh scenarioRunFixture, for the caller to turn into a
// golden comparable.
func runGoScenarioCase(t *testing.T, c ScenarioRunCase) scenario.ScenarioResult {
	t.Helper()

	def, err := script.ParseScenario(c.ScenarioYaml)
	if err != nil {
		t.Fatalf("Go ParseScenario: %v", err)
	}

	orch, clock := newScenarioRunFixture()
	r := scenario.NewScenarioRunner(scenario.ScenarioRunnerDeps{
		Orchestra:   orch,
		Clock:       clock,
		Logger:      noopLogger{},
		Commands:    runner.DefaultCommands(),
		LoadScript:  scenario.MemoryScriptLoader(c.Scripts),
		ExternalEnv: c.ExternalEnv,
	})

	result, runErr := r.Run(context.Background(), def)
	if runErr != nil {
		t.Fatalf("Go Run: %v", runErr)
	}
	return result
}

func TestScenarioRunDifferential(t *testing.T) {
	for _, c := range scenarioRunCorpus() {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			result := runGoScenarioCase(t, c)

			goJSON, err := json.Marshal(goScenarioResultComparable(result))
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}

			checkGolden(t, filepath.Join("scenario_run", c.Name+".json"), goJSON, DiffRunnerJSON)
		})
	}
}
