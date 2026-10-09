package report

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

func junitStr(s string) *string { return &s }

func mustJUnit(t *testing.T, input ReportInput) string {
	t.Helper()
	out, err := RenderJUnit(input)
	if err != nil {
		t.Fatalf("RenderJUnit: %v", err)
	}
	return string(out)
}

func TestRenderJUnit_PassedRun(t *testing.T) {
	got := mustJUnit(t, ScriptReportInput{
		Metadata: ReportMetadata{StartedAt: "2026-10-09T10:00:00Z"},
		Result:   runner.ScriptResult{ScriptName: "Login", Ok: true, TotalSteps: 2, PassedSteps: 2, DurationMs: 1500},
	})
	want := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="Login" tests="1" failures="0" errors="0" skipped="0" time="1.500" timestamp="2026-10-09T10:00:00Z">
  <testcase classname="Login" name="Login" time="1.500"></testcase>
</testsuite>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderJUnit_FailedRunNamesTheFailingStep(t *testing.T) {
	got := mustJUnit(t, ScriptReportInput{
		Result: runner.ScriptResult{
			ScriptName: "Login", DurationMs: 250, TotalSteps: 3, PassedSteps: 1, FailedAtStep: junitInt(1),
			Steps: []runner.StepResult{
				{StepIndex: 0, Command: "launchApp", Ok: true},
				{StepIndex: 1, Command: "tap", Ok: false, Detail: junitStr("no element matched")},
			},
		},
	})
	want := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="Login" tests="1" failures="1" errors="0" skipped="0" time="0.250">
  <testcase classname="Login" name="Login" time="0.250">
    <failure message="step 2 (tap) failed: no element matched">step 2 (tap) failed: no element matched</failure>
  </testcase>
</testsuite>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func junitInt(i int) *int { return &i }

func TestRenderJUnit_AbortedRunCarriesTheAbortReason(t *testing.T) {
	got := mustJUnit(t, ScriptReportInput{
		Result: runner.ScriptResult{
			ScriptName: "Login", FailedAtStep: junitInt(0),
			Abort: &runner.RunAbort{Reason: runner.RunAbortDeviceLost, Message: "device went away"},
		},
	})
	if !strings.Contains(got, `message="step 1 failed, aborted: device went away"`) {
		t.Errorf("abort reason missing:\n%s", got)
	}
}

func TestRenderJUnit_FailedScenarioNamesTheChildScript(t *testing.T) {
	got := mustJUnit(t, ScenarioReportInput{
		Result: scenario.ScenarioResult{
			ScenarioName: "Checkout", TotalScripts: 2, PassedScripts: 1,
			Scripts: []scenario.ScenarioScriptResult{
				{ScriptPath: "a.yml", Status: scenario.StatusPassed},
				{ScriptPath: "b.yml", Status: scenario.StatusErrored, Error: junitStr("cannot load")},
			},
		},
	})
	if !strings.Contains(got, `<failure message="script b.yml: cannot load">`) || !strings.Contains(got, `failures="1"`) {
		t.Errorf("scenario failure missing:\n%s", got)
	}
}

func junitFailedScript(step, detail string) *scenario.SuiteEntryOutcome {
	return &scenario.SuiteEntryOutcome{Script: &runner.ScriptResult{
		Steps: []runner.StepResult{{StepIndex: 0, Command: step, Ok: false, Detail: junitStr(detail)}},
	}}
}

func mixedSuite() SuiteReportInput {
	name := "nightly"
	return SuiteReportInput{
		Metadata: ReportMetadata{StartedAt: "2026-10-09T10:00:00Z"},
		Result: scenario.SuiteResult{
			SuiteName: &name, DurationMs: 9000, Total: 5, Passed: 2, Failed: 2, QuarantinedFailures: 1, Flaky: 1,
			Entries: []scenario.SuiteEntryResult{
				{Path: "ok.yml", Status: scenario.EntryPassed, DurationMs: 1000},
				{
					Path: "flaky.yml", Status: scenario.EntryPassed, Flaky: true, DurationMs: 3000,
					Attempts: []scenario.SuiteAttempt{
						{Number: 1, Status: scenario.EntryFailed, DurationMs: 1200, Result: junitFailedScript("tap", "slow")},
						{Number: 2, Status: scenario.EntryPassed, DurationMs: 1800},
					},
				},
				{
					Path: "quarantined.yml", Status: scenario.EntryFailed, Quarantined: true, DurationMs: 500,
					Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, DurationMs: 500, Result: junitFailedScript("tap", "boom")}},
				},
				{
					Path: "broken.yml", Status: scenario.EntryFailed, DurationMs: 2500,
					Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, DurationMs: 2500, Result: junitFailedScript("swipe", "off screen")}},
				},
				{
					Path: "unloadable.yml", Status: scenario.EntryFailed, DurationMs: 2,
					Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, DurationMs: 2, Error: junitStr("parse error")}},
				},
			},
		},
	}
}

func TestRenderJUnit_SuiteWithFlakyQuarantinedAndFailedEntries(t *testing.T) {
	got := mustJUnit(t, mixedSuite())
	want := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="nightly" tests="5" failures="1" errors="1" skipped="1" time="9.000" timestamp="2026-10-09T10:00:00Z">
  <testcase classname="nightly" name="ok.yml" time="1.000"></testcase>
  <testcase classname="nightly" name="flaky.yml" time="3.000">
    <flakyFailure message="step 1 (tap) failed: slow" time="1.200">step 1 (tap) failed: slow</flakyFailure>
  </testcase>
  <testcase classname="nightly" name="quarantined.yml" time="0.500">
    <skipped message="quarantined"></skipped>
  </testcase>
  <testcase classname="nightly" name="broken.yml" time="2.500">
    <failure message="step 1 (swipe) failed: off screen">step 1 (swipe) failed: off screen</failure>
  </testcase>
  <testcase classname="nightly" name="unloadable.yml" time="0.002">
    <error message="parse error">parse error</error>
  </testcase>
</testsuite>
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderJUnit_CountsAgreeWithTheExitCode(t *testing.T) {
	input := mixedSuite()
	var parsed struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Errors   int `xml:"errors,attr"`
		Skipped  int `xml:"skipped,attr"`
	}
	if err := xml.Unmarshal([]byte(mustJUnit(t, input)), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Tests != input.Result.Total {
		t.Errorf("tests = %d, want %d", parsed.Tests, input.Result.Total)
	}
	if parsed.Failures+parsed.Errors != input.Result.Failed {
		t.Errorf("failures+errors = %d, want the suite's %d failed entries", parsed.Failures+parsed.Errors, input.Result.Failed)
	}
	if parsed.Skipped != input.Result.QuarantinedFailures {
		t.Errorf("skipped = %d, want %d", parsed.Skipped, input.Result.QuarantinedFailures)
	}
}

func TestRenderJUnit_SuiteWithoutNameUsesTheDefault(t *testing.T) {
	got := mustJUnit(t, SuiteReportInput{Result: scenario.SuiteResult{
		Total: 1, Entries: []scenario.SuiteEntryResult{{Path: "a.yml", Status: scenario.EntryPassed}},
	}})
	if !strings.Contains(got, `<testsuite name="suite"`) || !strings.Contains(got, `classname="suite" name="a.yml"`) {
		t.Errorf("default suite name missing:\n%s", got)
	}
}

func TestRenderJUnit_EscapesMarkupAndControlCharacters(t *testing.T) {
	got := mustJUnit(t, SuiteReportInput{Result: scenario.SuiteResult{
		Total: 1, Failed: 1, Entries: []scenario.SuiteEntryResult{{
			Path: `a"<b>&.yml`, Status: scenario.EntryFailed,
			Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed, Error: junitStr("bad <tag> & \"quote\"\x00 end")}},
		}},
	}})
	var parsed struct {
		Cases []struct {
			Name  string `xml:"name,attr"`
			Error struct {
				Message string `xml:"message,attr"`
			} `xml:"error"`
		} `xml:"testcase"`
	}
	if err := xml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("output is not well-formed XML: %v\n%s", err, got)
	}
	if parsed.Cases[0].Name != `a"<b>&.yml` {
		t.Errorf("name = %q", parsed.Cases[0].Name)
	}
	if !strings.HasPrefix(parsed.Cases[0].Error.Message, `bad <tag> & "quote"`) || strings.Contains(parsed.Cases[0].Error.Message, "\x00") {
		t.Errorf("message = %q", parsed.Cases[0].Error.Message)
	}
	if strings.Contains(got, "<b>") {
		t.Errorf("raw markup leaked into the output:\n%s", got)
	}
}

func TestFsReporter_WritesJUnitOnlyWhenRequested(t *testing.T) {
	runDir := t.TempDir()
	reportDir := filepath.Join(runDir, "report")
	options := ReportOptions{Formats: []string{FormatJUnit}, ReportDir: reportDir, ArtifactsDir: filepath.Join(runDir, "artifacts")}
	input := ScriptReportInput{Result: runner.ScriptResult{ScriptName: "Login", Ok: true}}

	bundle, err := NewFsReporter().Generate(input, options)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(reportDir, "junit.xml")
	if len(bundle.Files) != 1 || bundle.Files[0] != want {
		t.Fatalf("files = %v, want [%s]", bundle.Files, want)
	}
	if _, err := os.Stat(filepath.Join(reportDir, "report.html")); err == nil {
		t.Error("report.html written without being requested")
	}
}
