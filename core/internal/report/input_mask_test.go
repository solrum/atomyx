package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

const maskTestSecret = "plant-7c1e9d42-secret"

func maskTestMask(s string) string { return strings.ReplaceAll(s, maskTestSecret, "[MASK]") }

func leakyScriptResult() runner.ScriptResult {
	detail := "step failed for " + maskTestSecret
	return runner.ScriptResult{
		ScriptName: "leaky", Steps: []runner.StepResult{{Command: "tap", Detail: &detail}},
		TotalSteps: 1, FailedAtStep: new(int),
		Abort: &runner.RunAbort{Message: "lost " + maskTestSecret},
	}
}

func readAll(t *testing.T, dir string) string {
	t.Helper()
	var all strings.Builder
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		all.Write(body)
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.String()
}

func generateWithMask(t *testing.T, input ReportInput, mask func(string) string) string {
	t.Helper()
	dir := t.TempDir()
	_, err := NewFsReporter().Generate(input, ReportOptions{
		Formats: []string{FormatHTML, FormatJSON, FormatJUnit}, ReportDir: filepath.Join(dir, "report"),
		ArtifactsDir: filepath.Join(dir, "artifacts"), Mask: mask,
	})
	if err != nil {
		t.Fatal(err)
	}
	return readAll(t, dir)
}

func TestReporterMasksTextFieldsOfEveryReportKindInEveryFormat(t *testing.T) {
	failure := "boom " + maskTestSecret
	script := leakyScriptResult()
	inputs := map[string]ReportInput{
		"script": ScriptReportInput{Result: script},
		"scenario": ScenarioReportInput{Result: scenario.ScenarioResult{
			Scripts: []scenario.ScenarioScriptResult{
				{Status: scenario.StatusFailed, Result: &script},
				{Status: scenario.StatusErrored, Error: &failure},
			},
		}},
		"suite": SuiteReportInput{Result: scenario.SuiteResult{Entries: []scenario.SuiteEntryResult{
			{Path: "a.yml", Kind: scenario.KindScript, Status: scenario.EntryFailed, Error: &failure,
				Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, Error: &failure, Failure: failure}}},
			{Path: "b.yml", Kind: scenario.KindScript, Status: scenario.EntryFailed,
				Result:   &scenario.SuiteEntryOutcome{Script: &script},
				Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, Result: &scenario.SuiteEntryOutcome{Script: &script}}}},
		}}},
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(generateWithMask(t, input, nil), maskTestSecret) {
				t.Fatal("the fixture never reaches a report, so the masked run proves nothing")
			}
			if out := generateWithMask(t, input, maskTestMask); strings.Contains(out, maskTestSecret) {
				t.Errorf("a report carries the secret:\n%s", out)
			}
		})
	}
}

func TestReporterMasksTheAbortOfTheMetadata(t *testing.T) {
	input := ScriptReportInput{
		Metadata: ReportMetadata{Abort: &ReportAbort{Reason: "x", Message: maskTestSecret, Remedy: maskTestSecret}},
		Result:   runner.ScriptResult{ScriptName: "s"},
	}
	if out := generateWithMask(t, input, maskTestMask); strings.Contains(out, maskTestSecret) {
		t.Errorf("a report carries the secret:\n%s", out)
	}
}

func TestMaskingLeavesTheOriginalResultUntouched(t *testing.T) {
	script := leakyScriptResult()
	script.Masked(maskTestMask)
	if !strings.Contains(*script.Steps[0].Detail, maskTestSecret) || !strings.Contains(script.Abort.Message, maskTestSecret) {
		t.Error("Masked rewrote its receiver")
	}
}
