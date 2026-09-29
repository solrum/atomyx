package conformance

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// toReportInput builds the report.ReportInput the Go report package
// renders from, out of the case's canonical fixture fields. It
// dispatches on Kind because Result's shape depends on it.
func (c *ReportCase) toReportInput() (report.ReportInput, error) {
	metadata := report.ReportMetadata{
		RunID:      c.Metadata.RunID,
		Label:      c.Metadata.Label,
		StartedAt:  c.Metadata.StartedAt,
		FinishedAt: c.Metadata.FinishedAt,
		DurationMs: c.Metadata.DurationMs,
		Command:    report.ReportCommand(c.Metadata.Command),
		Env:        c.Metadata.Env,
		Platform:   c.Metadata.Platform,
		Device:     c.Metadata.Device,
	}
	screenshots := make([]report.ReportScreenshot, len(c.Screenshots))
	for i, s := range c.Screenshots {
		screenshots[i] = report.ReportScreenshot{Scope: s.Scope, Label: s.Label}
	}

	switch c.Kind {
	case "", "script":
		var result reportCaseScriptResult
		if err := json.Unmarshal(c.Result, &result); err != nil {
			return nil, fmt.Errorf("report corpus case %s: decoding script result: %w", c.Name, err)
		}
		return report.ScriptReportInput{
			Metadata:    metadata,
			Result:      toRunnerScriptResult(result),
			Screenshots: screenshots,
		}, nil
	case "scenario":
		var result reportCaseScenarioResult
		if err := json.Unmarshal(c.Result, &result); err != nil {
			return nil, fmt.Errorf("report corpus case %s: decoding scenario result: %w", c.Name, err)
		}
		return report.ScenarioReportInput{
			Metadata:    metadata,
			Result:      toScenarioResult(result),
			Screenshots: screenshots,
		}, nil
	case "suite":
		var result reportCaseSuiteResult
		if err := json.Unmarshal(c.Result, &result); err != nil {
			return nil, fmt.Errorf("report corpus case %s: decoding suite result: %w", c.Name, err)
		}
		suiteResult, err := toSuiteResult(c.Name, result)
		if err != nil {
			return nil, err
		}
		return report.SuiteReportInput{
			Metadata:    metadata,
			Result:      suiteResult,
			Screenshots: screenshots,
		}, nil
	default:
		return nil, fmt.Errorf("report corpus case %s: unknown kind %q", c.Name, c.Kind)
	}
}

func toRunnerScriptResult(result reportCaseScriptResult) runner.ScriptResult {
	steps := make([]runner.StepResult, len(result.Steps))
	for i, s := range result.Steps {
		steps[i] = runner.StepResult{
			StepIndex:  s.StepIndex,
			Command:    s.Command,
			Ok:         s.Ok,
			Detail:     s.Detail,
			DurationMs: s.DurationMs,
		}
	}
	return runner.ScriptResult{
		ScriptName:   result.ScriptName,
		Ok:           result.Ok,
		Steps:        steps,
		TotalSteps:   result.TotalSteps,
		PassedSteps:  result.PassedSteps,
		FailedAtStep: result.FailedAtStep,
		DurationMs:   result.DurationMs,
	}
}

func toScenarioResult(result reportCaseScenarioResult) scenario.ScenarioResult {
	scripts := make([]scenario.ScenarioScriptResult, len(result.Scripts))
	for i, child := range result.Scripts {
		var scriptResult *runner.ScriptResult
		if child.Result != nil {
			r := toRunnerScriptResult(*child.Result)
			scriptResult = &r
		}
		scripts[i] = scenario.ScenarioScriptResult{
			ScriptIndex: child.ScriptIndex,
			ScriptPath:  child.ScriptPath,
			Status:      scenario.ScriptRunStatus(child.Status),
			Result:      scriptResult,
			Error:       child.Error,
			DurationMs:  child.DurationMs,
		}
	}
	return scenario.ScenarioResult{
		ScenarioName:  result.ScenarioName,
		Ok:            result.Ok,
		Scripts:       scripts,
		TotalScripts:  result.TotalScripts,
		PassedScripts: result.PassedScripts,
		DurationMs:    result.DurationMs,
	}
}

func toSuiteResult(caseName string, result reportCaseSuiteResult) (scenario.SuiteResult, error) {
	entries := make([]scenario.SuiteEntryResult, len(result.Entries))
	for i, entry := range result.Entries {
		e := scenario.SuiteEntryResult{
			Path:       entry.Path,
			Kind:       scenario.SuiteEntryKind(entry.Kind),
			Status:     scenario.SuiteEntryStatus(entry.Status),
			Error:      entry.Error,
			DurationMs: entry.DurationMs,
		}
		if len(entry.Result) > 0 {
			outcome, err := toSuiteEntryOutcome(entry.Kind, entry.Result)
			if err != nil {
				return scenario.SuiteResult{}, fmt.Errorf("report corpus case %s: entry %q: %w", caseName, entry.Path, err)
			}
			e.Result = &outcome
		}
		entries[i] = e
	}
	return scenario.SuiteResult{
		SuiteName:  result.SuiteName,
		Ok:         result.Ok,
		Entries:    entries,
		Total:      result.Total,
		Passed:     result.Passed,
		Failed:     result.Failed,
		DurationMs: result.DurationMs,
	}, nil
}

func toSuiteEntryOutcome(kind string, raw json.RawMessage) (scenario.SuiteEntryOutcome, error) {
	switch kind {
	case "scenario":
		var result reportCaseScenarioResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return scenario.SuiteEntryOutcome{}, fmt.Errorf("decoding scenario entry result: %w", err)
		}
		sc := toScenarioResult(result)
		return scenario.SuiteEntryOutcome{Scenario: &sc}, nil
	case "script":
		var result reportCaseScriptResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return scenario.SuiteEntryOutcome{}, fmt.Errorf("decoding script entry result: %w", err)
		}
		s := toRunnerScriptResult(result)
		return scenario.SuiteEntryOutcome{Script: &s}, nil
	default:
		return scenario.SuiteEntryOutcome{}, fmt.Errorf("unknown suite entry kind %q", kind)
	}
}

// goReportResponse replays a ReportCase against the Go report
// package, producing a raw ReportJson for a structural diff against
// the frozen golden fixture.
func goReportResponse(t *testing.T, c *ReportCase) json.RawMessage {
	t.Helper()

	input, err := c.toReportInput()
	if err != nil {
		t.Fatalf("building Go report input: %v", err)
	}
	index := report.BuildScreenshotIndex(input.ReportScreenshots())
	result, err := report.RenderJSON(input, index.ByScope)
	if err != nil {
		t.Fatalf("Go renderJson: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling Go renderJson result: %v", err)
	}
	return raw
}

// TestReportDifferential diffs the Go report package's RenderJSON
// output over every corpus/report case against a frozen golden
// fixture, covering a passing multi-step script with screenshots
// across scopes, a script that fails partway through, a scenario with
// mixed child outcomes, and a suite with mixed script and scenario
// entries.
func TestReportDifferential(t *testing.T) {
	cases, err := LoadReportCorpus("corpus/report")
	if err != nil {
		t.Fatalf("loading report corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			goResult := goReportResponse(t, c)
			checkGolden(t, filepath.Join("report", c.Name+".json"), goResult, DiffJSON)
		})
	}
}
