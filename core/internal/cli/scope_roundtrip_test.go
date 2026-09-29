package cli

import (
	"testing"

	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// fakeScreenshotArtifacts is a minimal runner.ScriptArtifacts that
// holds one screenshot, so a fixture ScriptResult can carry a
// screenshot without running an actual script.
type fakeScreenshotArtifacts struct {
	shots []runner.Screenshot
}

func (a *fakeScreenshotArtifacts) AddScreenshot(label string, data []byte, mimeType string) {
	a.shots = append(a.shots, runner.Screenshot{Label: label, Data: data, MimeType: mimeType})
}

func (a *fakeScreenshotArtifacts) Screenshots() []runner.Screenshot {
	return a.shots
}

func scriptResultWithScreenshot(name, label string) runner.ScriptResult {
	arts := &fakeScreenshotArtifacts{}
	arts.AddScreenshot(label, []byte("fake-png-bytes"), "image/png")
	return runner.ScriptResult{ScriptName: name, Ok: true, Artifacts: arts}
}

// TestSuiteScreenshotScope_ProducerMatchesConsumerIndex proves that
// suiteReportScreenshots (the CLI-side producer of report.ReportScreenshot
// scopes) and report.BuildScreenshotIndex plus report.RenderJSON (the
// report package's consumer side) key every screenshot to the same
// scope. If suiteReportScreenshots ever built a scope with a
// different format than report.ScenarioChildScope / report.SuiteEntryScope
// produce, BuildScreenshotIndex's ByScope map would file the
// screenshot under a key RenderJSON never looks up, and every
// Screenshots assertion below would see an empty slice instead of one
// entry.
func TestSuiteScreenshotScope_ProducerMatchesConsumerIndex(t *testing.T) {
	addToCart := scriptResultWithScreenshot("add-to-cart", "cart")
	pay := scriptResultWithScreenshot("pay", "paid")
	scenarioResult := scenario.ScenarioResult{
		ScenarioName: "checkout",
		Ok:           true,
		Scripts: []scenario.ScenarioScriptResult{
			{ScriptIndex: 0, ScriptPath: "scripts/add-to-cart.yml", Status: scenario.StatusPassed, Result: &addToCart},
			{ScriptIndex: 1, ScriptPath: "scripts/pay.yml", Status: scenario.StatusPassed, Result: &pay},
		},
		TotalScripts:  2,
		PassedScripts: 2,
	}

	smoke := scriptResultWithScreenshot("smoke", "home")

	suiteResult := scenario.SuiteResult{
		Ok: true,
		Entries: []scenario.SuiteEntryResult{
			{
				Path:   "scenarios/checkout.yml",
				Kind:   scenario.KindScenario,
				Status: scenario.EntryPassed,
				Result: &scenario.SuiteEntryOutcome{Scenario: &scenarioResult},
			},
			{
				Path:   "scripts/smoke.yml",
				Kind:   scenario.KindScript,
				Status: scenario.EntryPassed,
				Result: &scenario.SuiteEntryOutcome{Script: &smoke},
			},
		},
		Total:  2,
		Passed: 2,
	}

	screenshots := suiteReportScreenshots(suiteResult)
	if len(screenshots) != 3 {
		t.Fatalf("expected 3 flattened screenshots, got %d", len(screenshots))
	}

	index := report.BuildScreenshotIndex(screenshots)

	input := report.SuiteReportInput{
		Metadata: report.ReportMetadata{
			RunID:   "test-run",
			Label:   "suite",
			Command: report.ReportCommandSuite,
		},
		Result:      suiteResult,
		Screenshots: screenshots,
	}
	rendered, err := report.RenderJSON(input, index.ByScope)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	suiteJSON, ok := rendered.Result.(report.ReportJsonSuiteResult)
	if !ok {
		t.Fatalf("expected a ReportJsonSuiteResult, got %T", rendered.Result)
	}

	scenarioJSON, ok := suiteJSON.Entries[0].Result.(report.ReportJsonScenarioResult)
	if !ok {
		t.Fatalf("expected entry 0 to hold a ReportJsonScenarioResult, got %T", suiteJSON.Entries[0].Result)
	}
	assertSingleScreenshot(t, "add-to-cart child", scenarioJSON.Scripts[0].Result.Screenshots, "cart")
	assertSingleScreenshot(t, "pay child", scenarioJSON.Scripts[1].Result.Screenshots, "paid")

	scriptJSON, ok := suiteJSON.Entries[1].Result.(report.ReportJsonScriptResult)
	if !ok {
		t.Fatalf("expected entry 1 to hold a ReportJsonScriptResult, got %T", suiteJSON.Entries[1].Result)
	}
	assertSingleScreenshot(t, "smoke entry", scriptJSON.Screenshots, "home")
}

func assertSingleScreenshot(t *testing.T, what string, refs []report.ReportJsonScreenshotRef, wantLabel string) {
	t.Helper()
	if len(refs) != 1 {
		t.Fatalf("%s: expected 1 screenshot resolved through the scope index, got %d", what, len(refs))
	}
	if refs[0].Label != wantLabel {
		t.Errorf("%s: expected label %q, got %q", what, wantLabel, refs[0].Label)
	}
	if refs[0].File == "" {
		t.Errorf("%s: expected a non-empty resolved file name", what)
	}
}
