package report

import (
	"encoding/json"
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
)

func scriptInputWithTypeStep(detail *string, ok bool) ScriptReportInput {
	return ScriptReportInput{
		Metadata: ReportMetadata{RunID: "r1", Label: "Login flow", Command: ReportCommandRun},
		Result: runner.ScriptResult{
			ScriptName: "Login flow",
			Ok:         ok,
			Steps: []runner.StepResult{
				{StepIndex: 0, Command: "type", Ok: ok, Detail: detail, DurationMs: 10},
			},
			TotalSteps:  1,
			PassedSteps: 1,
			DurationMs:  10,
		},
	}
}

// TestRenderJSON_RedactsSuccessfulTypeStepDetail guards the report's
// persisted JSON against ever carrying a typed literal, which could be
// a secret resolved from a config environment variable: RenderJSON
// must substitute redactedTypeDetail for a successful type step's
// Detail regardless of what the runner reported.
func TestRenderJSON_RedactsSuccessfulTypeStepDetail(t *testing.T) {
	const secret = "s3cr3t-9f3a"
	detail := "Typed \"" + secret + "\""
	input := scriptInputWithTypeStep(&detail, true)

	rendered, err := RenderJSON(input, ScreenshotsByScope{})
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	encoded, err := json.Marshal(rendered)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("rendered report.json contains the typed literal: %s", encoded)
	}

	scriptResult, ok := rendered.Result.(ReportJsonScriptResult)
	if !ok {
		t.Fatalf("Result is %T, want ReportJsonScriptResult", rendered.Result)
	}
	if got := *scriptResult.Steps[0].Detail; got != redactedTypeDetail {
		t.Fatalf("step detail = %q, want %q", got, redactedTypeDetail)
	}
}

// TestRenderJSON_KeepsFailedTypeStepDetail confirms a failed type
// step's Detail, which names why the step failed rather than the
// value it tried to type, is left untouched: there is nothing to
// redact, and the failure reason is useful diagnostic information.
func TestRenderJSON_KeepsFailedTypeStepDetail(t *testing.T) {
	reason := "element not found"
	input := scriptInputWithTypeStep(&reason, false)

	rendered, err := RenderJSON(input, ScreenshotsByScope{})
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	scriptResult, ok := rendered.Result.(ReportJsonScriptResult)
	if !ok {
		t.Fatalf("Result is %T, want ReportJsonScriptResult", rendered.Result)
	}
	if got := *scriptResult.Steps[0].Detail; got != reason {
		t.Fatalf("step detail = %q, want the unredacted failure reason %q", got, reason)
	}
}

// TestRenderHTML_RedactsSuccessfulTypeStepDetail is RenderJSON's
// counterpart for the HTML report: the rendered bytes must not
// contain the typed literal either.
func TestRenderHTML_RedactsSuccessfulTypeStepDetail(t *testing.T) {
	const secret = "s3cr3t-9f3a"
	detail := "Typed \"" + secret + "\""
	input := scriptInputWithTypeStep(&detail, true)

	html, err := RenderHTML(input, ScreenshotsByScope{}, "")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if strings.Contains(html, secret) {
		t.Fatalf("rendered report.html contains the typed literal:\n%s", html)
	}
	if !strings.Contains(html, redactedTypeDetail) {
		t.Fatalf("rendered report.html missing the redacted placeholder:\n%s", html)
	}
}
