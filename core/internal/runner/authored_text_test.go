package runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/script"
)

const envSecret = "envS3cr3t77aa"

// authoredRun is a run of a script whose placeholders expand to envSecret,
// with every step result and step event it produced.
type authoredRun struct {
	result ScriptResult
	events []StepEvent
}

func runWithEnvSecret(t *testing.T, tr *testRunner, yaml string) authoredRun {
	t.Helper()
	def, err := script.ParseScript(yaml, map[string]string{"SECRET": envSecret, "APP": "com.test", "KEY": "back"})
	if err != nil {
		t.Fatal(err)
	}
	var out authoredRun
	tr.runner.deps.OnStep = func(e StepEvent) { out.events = append(out.events, e) }
	result, err := tr.runner.Run(context.Background(), def)
	if err != nil {
		t.Fatal(err)
	}
	out.result = result
	return out
}

// text returns everything the run recorded about its steps: details,
// summaries and summary tokens.
func (r authoredRun) text() string {
	var parts []string
	for _, s := range r.result.Steps {
		parts = append(parts, detail(s))
	}
	for _, e := range r.events {
		parts = append(parts, e.Summary, detailOrEmpty(e.Detail))
		for _, tok := range e.Tokens {
			parts = append(parts, tok.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func requireWrittenNotExpanded(t *testing.T, run authoredRun, wantPlaceholder string) {
	t.Helper()
	text := run.text()
	if strings.Contains(text, envSecret) {
		t.Fatalf("a step record carries the expanded value:\n%s", text)
	}
	if !strings.Contains(text, wantPlaceholder) {
		t.Fatalf("a step record lacks %q as written:\n%s", wantPlaceholder, text)
	}
}

func TestAssertExprDetailQuotesTheExpressionAsWritten(t *testing.T) {
	for name, expr := range map[string]string{
		"passes":        `\"${SECRET}\" == \"${SECRET}\"`,
		"does not hold": `\"${SECRET}\" == \"other\"`,
		"not boolean":   `\"${SECRET}\"`,
		"syntax error":  `\"${SECRET}\" == \"a\" \"${SECRET}\"`,
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(rootTree())
			run := runWithEnvSecret(t, tr, "- assertExpr: { expr: \""+expr+"\" }\n")
			requireWrittenNotExpanded(t, run, "${SECRET}")
		})
	}
}

func TestSelectorSummaryAndFailureShowThePlaceholder(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	run := runWithEnvSecret(t, tr, "- tap: { text: \"${SECRET}\" }\n")
	if run.result.Ok {
		t.Fatal("tap of an absent element succeeded")
	}
	requireWrittenNotExpanded(t, run, `element.text = "${SECRET}"`)
}

func TestNestedStepsShowTheirPlaceholders(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "[0,0][10,10]")))
	run := runWithEnvSecret(t, tr, "- handle:\n"+
		"    - when: { visible: \"Go\" }\n"+
		"      do:\n"+
		"        - assertExpr: { expr: \"\\\"${SECRET}\\\" == \\\"${SECRET}\\\"\" }\n"+
		"    - otherwise: skip\n")
	if !run.result.Ok {
		t.Fatalf("handle failed: %s", debugSteps(run.result.Steps))
	}
	requireWrittenNotExpanded(t, run, "${SECRET}")
}

func TestAssertApiDetailQuotesTheExpectationAsWritten(t *testing.T) {
	for name, body := range map[string]string{
		"equality":   "$.token: ${SECRET}",
		"contains":   "$.token: $contains:${SECRET}",
		"between":    "$.count: $between:${SECRET},${SECRET}",
		"comparison": "$.count: $gt:${SECRET}",
		"operator":   "$.token: $${SECRET}",
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(rootTree())
			tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 200,
				Body: map[string]any{"token": "something", "count": float64(3)}}}
			run := runWithEnvSecret(t, tr, "appId: com.test\nproxy: required\n---\n"+
				"- capture: \"GET /x as: r\"\n- assertApi:\n    from: r\n    body:\n      "+body+"\n")
			if run.result.Ok {
				t.Fatal("assertion unexpectedly passed")
			}
			requireWrittenNotExpanded(t, run, "${SECRET}")
		})
	}
}

func TestCaptureReadScreenshotAndPressKeyDetailsShowTheirPlaceholders(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "[0,0][10,10]")))
	tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 200}}
	run := runWithEnvSecret(t, tr, "appId: com.test\nproxy: required\n---\n"+
		"- capture: { pattern: \"GET /x\", as: \"${SECRET}\" }\n"+
		"- screenshot: \"${SECRET}\"\n"+
		"- pressKey: \"${SECRET}\"\n")
	text := run.text()
	if strings.Contains(text, envSecret) {
		t.Fatalf("a step record carries the expanded value:\n%s", text)
	}
	for _, want := range []string{
		`as "${SECRET}"`, `Screenshot saved as "${SECRET}"`, `PressKey: "${SECRET}" is not in`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("step records lack %q:\n%s", want, text)
		}
	}
}

func TestReadDetailShowsTheVariableNameAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "[0,0][10,10]")))
	run := runWithEnvSecret(t, tr, "- read: { selector: { id: go }, field: text, as: \"${SECRET}\" }\n")
	if !run.result.Ok {
		t.Fatalf("read failed: %s", debugSteps(run.result.Steps))
	}
	if got := detail(lastStep(t, run.result)); got != `Read text into "${SECRET}"` {
		t.Fatalf("detail = %q", got)
	}
}

func TestCaptureTimeoutShowsThePatternAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	run := runWithEnvSecret(t, tr, "appId: com.test\nproxy: required\n---\n- capture: { pattern: \"GET /${SECRET}\", as: r }\n")
	if run.result.Ok {
		t.Fatal("capture with nothing staged succeeded")
	}
	requireWrittenNotExpanded(t, run, `"GET /${SECRET}"`)
}

func TestAssertApiNamesTheCaptureAsWritten(t *testing.T) {
	const flow = "appId: com.test\nproxy: required\n---\n"
	t.Run("missing capture", func(t *testing.T) {
		tr := newTestRunner()
		tr.driver.StageHierarchy(rootTree())
		run := runWithEnvSecret(t, tr, flow+"- assertApi:\n    from: ${SECRET}\n    status: 200\n")
		requireWrittenNotExpanded(t, run, `"${SECRET}"`)
	})
	t.Run("failed assertion", func(t *testing.T) {
		tr := newTestRunner()
		tr.driver.StageHierarchy(rootTree())
		tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 500}}
		run := runWithEnvSecret(t, tr, flow+"- capture: { pattern: \"GET /x\", as: \"${SECRET}\" }\n- assertApi:\n    from: ${SECRET}\n    status: 200\n")
		requireWrittenNotExpanded(t, run, `API assertion failed on "${SECRET}"`)
	})
}

func TestAssertApiThresholdsKeepTheirNumericWordingWithoutPlaceholders(t *testing.T) {
	for body, want := range map[string]string{
		"$.count: $between:1.0,2.50": "expected $between:1,2.5, got a number outside that range",
		"$.count: $gt:10.0":          "expected $gt:10, got a number that does not satisfy it",
	} {
		tr := newTestRunner()
		tr.driver.StageHierarchy(rootTree())
		tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 200, Body: map[string]any{"count": float64(5)}}}
		run := runWithEnvSecret(t, tr, "appId: com.test\nproxy: required\n---\n"+
			"- capture: \"GET /x as: r\"\n- assertApi:\n    from: r\n    body:\n      "+body+"\n")
		if got := detail(lastStep(t, run.result)); !strings.Contains(got, want) {
			t.Fatalf("detail = %q, want it to contain %q", got, want)
		}
	}
}

func TestAssertApiNumericPlaceholdersShowAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 200, Body: map[string]any{"count": float64(5)}}}
	def, err := script.ParseScript("appId: com.test\nproxy: required\n---\n"+
		"- capture: \"GET /x as: r\"\n- assertApi:\n    from: r\n    body:\n      $.count: $between:${LOW},${HIGH}\n",
		map[string]string{"LOW": "1", "HIGH": "2"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tr.runner.Run(context.Background(), def)
	if err != nil {
		t.Fatal(err)
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "expected $between:${LOW},${HIGH}, got a number outside") {
		t.Fatalf("detail = %q, want the thresholds as written", got)
	}
}

func TestRunFlowWithoutALoaderShowsThePathAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.runner.deps.FlowLoader = nil
	tr.driver.StageHierarchy(rootTree())
	run := runWithEnvSecret(t, tr, "- runFlow: { file: \"flows/${SECRET}.yml\" }\n")
	requireWrittenNotExpanded(t, run, filepath.Join(mustAbs(t, "flows"), "${SECRET}.yml"))
}

func TestMissingCaptureNamesTheVariableNotItsValue(t *testing.T) {
	for name, step := range map[string]string{
		"branch":  "- branch:\n    from: ${SECRET}\n    on:\n      - match: { status: 200 }\n        do:\n          - back\n",
		"extract": "- extract:\n    from: ${SECRET}\n    values:\n      v: $.body.x\n",
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(rootTree())
			run := runWithEnvSecret(t, tr, step)
			if run.result.Ok {
				t.Fatal("a step on an uncaptured request succeeded")
			}
			requireWrittenNotExpanded(t, run, "SECRET")
		})
	}
}

func TestRunFlowAndHandleFileFailuresShowThePathAsWritten(t *testing.T) {
	for name, step := range map[string]string{
		"runFlow": "- runFlow: { file: \"flows/${SECRET}.yml\" }\n",
		"handle":  "- handle:\n    - when: {}\n      do: \"flows/${SECRET}.yml\"\n    - otherwise: fail\n",
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(rootTree())
			run := runWithEnvSecret(t, tr, step)
			if run.result.Ok {
				t.Fatal("a missing flow file did not fail the step")
			}
			want := filepath.Join(mustAbs(t, "flows"), "${SECRET}.yml")
			requireWrittenNotExpanded(t, run, want)
		})
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestLaunchAppDetailShowsTheAppIDAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	run := runWithEnvSecret(t, tr, "appId: ${APP}\n---\n- launchApp\n")
	if !run.result.Ok {
		t.Fatalf("launch failed: %s", debugSteps(run.result.Steps))
	}
	if got := detail(lastStep(t, run.result)); got != "Launched ${APP}" {
		t.Fatalf("detail = %q, want the app id as written", got)
	}

	tr = newTestRunner()
	tr.driver.Apps = nil
	tr.driver.StageHierarchy(rootTree())
	run = runWithEnvSecret(t, tr, "appId: ${APP}\n---\n- launchApp\n")
	got := detail(lastStep(t, run.result))
	if strings.Contains(got, "com.test") || !strings.Contains(got, "${APP}") {
		t.Fatalf("not-installed detail = %q, want the app id as written", got)
	}
}

func TestPressKeySuccessShowsTheKeyAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	run := runWithEnvSecret(t, tr, "- pressKey: \"${KEY}\"\n")
	if !run.result.Ok {
		t.Fatalf("pressKey failed: %s", debugSteps(run.result.Steps))
	}
	if got := detail(lastStep(t, run.result)); got != "Pressed ${KEY}" {
		t.Fatalf("detail = %q, want the key as written", got)
	}
}

func TestExtractPathFailureShowsThePathAsWritten(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 200, Body: map[string]any{}}}
	run := runWithEnvSecret(t, tr, "appId: com.test\nproxy: required\n---\n"+
		"- capture: \"GET /x as: r\"\n- extract:\n    from: r\n    values:\n      v: \"$.body.${SECRET}\"\n")
	if run.result.Ok {
		t.Fatal("extract of an absent path succeeded")
	}
	requireWrittenNotExpanded(t, run, `Path "$.body.${SECRET}" resolved to undefined`)
}
