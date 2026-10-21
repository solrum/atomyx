package runner

import (
	"strings"
	"testing"
)

func extractRegexRun(t *testing.T, body map[string]any, extract string) ScriptResult {
	t.Helper()
	tr := newTestRunner()
	tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/m", Status: 200, Body: body}}
	return run(t, tr, "appId: com.test\nproxy: required\n---\n- capture: \"GET /m as: inbox\"\n"+extract)
}

func TestExtractRegex_UsesTheFirstCaptureGroup(t *testing.T) {
	result := extractRegexRun(t, map[string]any{"text": "Your code is 482913, valid for 10 minutes"}, `- extract:
    from: inbox
    values:
      otp:
        path: $.body.text
        regex: "code is (\\d{6})"
- assertExpr:
    expr: "otp == \"482913\""
`)
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
}

func TestExtractRegex_WithoutAGroupStoresTheWholeMatch(t *testing.T) {
	result := extractRegexRun(t, map[string]any{"text": "Your code is 482913."}, `- extract:
    from: inbox
    values:
      otp:
        path: $.body.text
        regex: "\\d{6}"
- assertExpr:
    expr: "otp == \"482913\""
`)
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
}

func TestExtractRegex_NoMatchNamesThePathAndPatternNotTheValue(t *testing.T) {
	result := extractRegexRun(t, map[string]any{"text": "private message body 99"}, `- extract:
    from: inbox
    values:
      otp:
        path: $.body.text
        regex: "\\b(\\d{6})\\b"
`)
	if result.Ok {
		t.Fatal("a pattern with no match must fail the step")
	}
	got := detail(lastStep(t, result))
	want := `Pattern "\b(\d{6})\b" did not match the value at path "$.body.text" in captured "inbox"`
	if got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
	if strings.Contains(got, "private message") {
		t.Errorf("detail leaks the value: %q", got)
	}
}

func TestExtractRegex_PlainPathStillWorksBesideAnObjectValue(t *testing.T) {
	result := extractRegexRun(t, map[string]any{"a": "x", "text": "n 12345"}, `- extract:
    from: inbox
    values:
      plain: $.body.a
      num:
        path: $.body.text
        regex: "(\\d+)"
- assertExpr:
    expr: "plain == \"x\""
- assertExpr:
    expr: "num == \"12345\""
`)
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
}

func TestExtract_MissingCaptureNamesBothSteps(t *testing.T) {
	result := run(t, newTestRunner(), "appId: com.test\n---\n- extract:\n    from: nothing\n    values:\n      v: $.body\n")
	want := `No captured request named "nothing". Run a "capture" or "httpRequest" step with "as: nothing" first.`
	if got := detail(lastStep(t, result)); got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}
