package report

import (
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

func TestReportRenderHTML_Script(t *testing.T) {
	detail := "tapped #submit"
	input := ScriptReportInput{
		Metadata: ReportMetadata{RunID: "r1", Label: "Login flow", Command: ReportCommandRun},
		Result: runner.ScriptResult{
			ScriptName: "Login flow",
			Ok:         true,
			Steps: []runner.StepResult{
				{StepIndex: 0, Command: "launchApp", Ok: true, DurationMs: 100},
				{StepIndex: 1, Command: "tap", Ok: true, Detail: &detail, DurationMs: 50},
			},
			TotalSteps:  2,
			PassedSteps: 2,
			DurationMs:  150,
		},
		Screenshots: []ReportScreenshot{
			{Scope: RootScriptScope, Label: "after login"},
			{Scope: RootScriptScope, Label: "second shot"},
		},
	}

	index := BuildScreenshotIndex(input.Screenshots)
	html, err := RenderHTML(input, index.ByScope, "screenshots/")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	if !strings.Contains(html, "Login flow") {
		t.Errorf("expected html to contain the script name, got:\n%s", html)
	}
	if got := strings.Count(html, "<tr class="); got != len(input.Result.Steps) {
		t.Errorf("expected %d step rows, got %d", len(input.Result.Steps), got)
	}
	if !strings.Contains(html, "launchApp") || !strings.Contains(html, "tap") {
		t.Errorf("expected html to contain every step command, got:\n%s", html)
	}
	if got := strings.Count(html, "<img src=\"screenshots/"); got != len(input.Screenshots) {
		t.Errorf("expected %d screenshot images, got %d", len(input.Screenshots), got)
	}
	if !strings.Contains(html, "01-after-login.png") || !strings.Contains(html, "02-second-shot.png") {
		t.Errorf("expected html to reference the indexed screenshot file names, got:\n%s", html)
	}
}

func TestReportRenderHTML_Scenario(t *testing.T) {
	verifyErr := "element not found: #verify"
	input := ScenarioReportInput{
		Metadata: ReportMetadata{RunID: "r2", Label: "Onboarding scenario", Command: ReportCommandRun},
		Result: scenario.ScenarioResult{
			ScenarioName:  "Onboarding scenario",
			Ok:            false,
			TotalScripts:  3,
			PassedScripts: 1,
			DurationMs:    900,
			Scripts: []scenario.ScenarioScriptResult{
				{
					ScriptIndex: 0,
					ScriptPath:  "scripts/sign-up.yml",
					Status:      scenario.StatusPassed,
					DurationMs:  400,
					Result: &runner.ScriptResult{
						ScriptName:  "Sign up",
						Ok:          true,
						Steps:       []runner.StepResult{{StepIndex: 0, Command: "tap", Ok: true, DurationMs: 400}},
						TotalSteps:  1,
						PassedSteps: 1,
						DurationMs:  400,
					},
				},
				{
					ScriptIndex: 1,
					ScriptPath:  "scripts/verify-email.yml",
					Status:      scenario.StatusFailed,
					DurationMs:  500,
					Result: &runner.ScriptResult{
						ScriptName:  "Verify email",
						Ok:          false,
						Steps:       []runner.StepResult{{StepIndex: 0, Command: "assertVisible", Ok: false, Detail: &verifyErr, DurationMs: 500}},
						TotalSteps:  1,
						PassedSteps: 0,
						DurationMs:  500,
					},
				},
				{
					ScriptIndex: 2,
					ScriptPath:  "scripts/complete-profile.yml",
					Status:      scenario.StatusSkipped,
					DurationMs:  0,
				},
			},
		},
		Screenshots: []ReportScreenshot{
			{Scope: "0::scripts/sign-up.yml", Label: "account created"},
		},
	}

	index := BuildScreenshotIndex(input.Screenshots)
	html, err := RenderHTML(input, index.ByScope, "screenshots/")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	if !strings.Contains(html, "Onboarding scenario") {
		t.Errorf("expected html to contain the scenario name, got:\n%s", html)
	}
	if got := strings.Count(html, "<article class=\"scenario-child\">"); got != len(input.Result.Scripts) {
		t.Errorf("expected %d scenario-child sections, got %d", len(input.Result.Scripts), got)
	}
	if !strings.Contains(html, "scripts/sign-up.yml") || !strings.Contains(html, "scripts/verify-email.yml") || !strings.Contains(html, "scripts/complete-profile.yml") {
		t.Errorf("expected html to contain every child script path, got:\n%s", html)
	}
	if got := strings.Count(html, "<img src=\"screenshots/"); got != 1 {
		t.Errorf("expected 1 screenshot image, got %d", got)
	}
}

func TestReportRenderHTML_Suite(t *testing.T) {
	input := SuiteReportInput{
		Metadata: ReportMetadata{RunID: "r3", Label: "Regression suite", Command: ReportCommandSuite},
		Result: scenario.SuiteResult{
			Ok:         false,
			Total:      2,
			Passed:     1,
			Failed:     1,
			DurationMs: 700,
			Entries: []scenario.SuiteEntryResult{
				{
					Path:       "scripts/smoke.yml",
					Kind:       scenario.KindScript,
					Status:     scenario.EntryPassed,
					DurationMs: 300,
					Result: &scenario.SuiteEntryOutcome{
						Script: &runner.ScriptResult{
							ScriptName:  "Smoke test",
							Ok:          true,
							Steps:       []runner.StepResult{{StepIndex: 0, Command: "launchApp", Ok: true, DurationMs: 300}},
							TotalSteps:  1,
							PassedSteps: 1,
							DurationMs:  300,
						},
					},
				},
				{
					Path:       "scenarios/checkout.yml",
					Kind:       scenario.KindScenario,
					Status:     scenario.EntryFailed,
					DurationMs: 400,
					Result: &scenario.SuiteEntryOutcome{
						Scenario: &scenario.ScenarioResult{
							ScenarioName:  "Checkout scenario",
							Ok:            false,
							TotalScripts:  1,
							PassedScripts: 0,
							DurationMs:    400,
							Scripts: []scenario.ScenarioScriptResult{
								{
									ScriptIndex: 0,
									ScriptPath:  "scripts/pay.yml",
									Status:      scenario.StatusFailed,
									DurationMs:  400,
									Result: &runner.ScriptResult{
										ScriptName:  "Pay",
										Ok:          false,
										Steps:       []runner.StepResult{{StepIndex: 0, Command: "tap", Ok: false, DurationMs: 400}},
										TotalSteps:  1,
										PassedSteps: 0,
										DurationMs:  400,
									},
								},
							},
						},
					},
				},
			},
		},
		Screenshots: []ReportScreenshot{
			{Scope: "0::scripts/smoke.yml", Label: "smoke start"},
			{Scope: "1::scenarios/checkout.yml::0::scripts/pay.yml", Label: "pay failed"},
		},
	}

	index := BuildScreenshotIndex(input.Screenshots)
	html, err := RenderHTML(input, index.ByScope, "screenshots/")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	if !strings.Contains(html, "Regression suite") {
		t.Errorf("expected html to fall back to the metadata label as the title, got:\n%s", html)
	}
	if got := strings.Count(html, "<article class=\"suite-entry\">"); got != len(input.Result.Entries) {
		t.Errorf("expected %d suite-entry sections, got %d", len(input.Result.Entries), got)
	}
	if !strings.Contains(html, "scripts/smoke.yml") || !strings.Contains(html, "scenarios/checkout.yml") {
		t.Errorf("expected html to contain every entry path, got:\n%s", html)
	}
	if !strings.Contains(html, "0 of 1 scripts passed") || !strings.Contains(html, "scripts/pay.yml") {
		t.Errorf("expected html to render the nested scenario entry's own child, got:\n%s", html)
	}
	if got := strings.Count(html, "<img src=\"screenshots/"); got != 2 {
		t.Errorf("expected 2 screenshot images, got %d", got)
	}
}

func TestReportRenderHTML_UnsupportedKind(t *testing.T) {
	if _, err := RenderHTML(fakeReportInput{}, ScreenshotsByScope{}, ""); err == nil {
		t.Fatal("expected RenderHTML to error on an unsupported report kind")
	}
}

func TestReportRenderJSON_UnsupportedKind(t *testing.T) {
	if _, err := RenderJSON(fakeReportInput{}, ScreenshotsByScope{}); err == nil {
		t.Fatal("expected RenderJSON to error on an unsupported report kind")
	}
}

// fakeReportInput implements ReportInput as a Go type none of
// RenderHTML's or RenderJSON's type-switch cases match, to exercise
// their typed-error path regardless of how many kinds they support.
type fakeReportInput struct{}

func (fakeReportInput) ReportKind() ReportKind                { return "unknown" }
func (fakeReportInput) ReportMetadata() ReportMetadata        { return ReportMetadata{} }
func (fakeReportInput) ReportScreenshots() []ReportScreenshot { return nil }
