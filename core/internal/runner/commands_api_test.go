package runner

import (
	"strings"
	"testing"

	"dev.atomyx/internal/wire"
)

func TestCaptureAssertApiExtract_HappyPath(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{
		Method: "POST",
		URL:    "/api/v1/auth/login",
		Status: 200,
		Body: map[string]any{
			"user": map[string]any{
				"id":    float64(42),
				"email": "qa@atomyx.test",
			},
		},
	}}

	yaml := "appId: com.test\nproxy: required\n---\n" +
		"- capture: \"POST /api/v1/auth/login as: loginRsp\"\n" +
		"- assertApi:\n" +
		"    from: loginRsp\n" +
		"    status: 200\n" +
		"    body:\n" +
		"      $.user.email: qa@atomyx.test\n" +
		"- extract:\n" +
		"    from: loginRsp\n" +
		"    values:\n" +
		"      userId: $.body.user.id\n" +
		"- assertExpr:\n" +
		"    expr: \"userId == \\\"42\\\"\"\n"

	result := run(t, tr, yaml)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if result.PassedSteps != 4 {
		t.Fatalf("got PassedSteps=%d, want 4", result.PassedSteps)
	}
}

func TestAssertApiCommand_NoCapture(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- assertApi:\n    from: missing\n    status: 200\n")
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "No captured request named \"missing\". Did you run a \"capture\" step with \"as: missing\" before this assertion?"
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestAssertApiCommand_OperatorFailureMessage(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{
		Method: "GET", URL: "/x", Status: 200,
		Body: map[string]any{"name": ""},
	}}

	yaml := "appId: com.test\nproxy: required\n---\n" +
		"- capture: \"GET /x as: r\"\n" +
		"- assertApi:\n" +
		"    from: r\n" +
		"    body:\n" +
		"      $.name: $not_empty\n"
	result := run(t, tr, yaml)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "API assertion failed on \"r\":\n  - body $.name: expected $not_empty, got a string of 0 characters"
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestExtractCommand_UndefinedPath(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{Method: "GET", URL: "/x", Status: 200, Body: map[string]any{}}}

	yaml := "appId: com.test\nproxy: required\n---\n" +
		"- capture: \"GET /x as: r\"\n" +
		"- extract:\n" +
		"    from: r\n" +
		"    values:\n" +
		"      missing: $.nope\n"
	result := run(t, tr, yaml)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "Path \"$.nope\" resolved to undefined in captured \"r\""
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestBranchCommand_RoutesOnStatus(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("dash", "Dashboard", "0,0,10,10")))
	tr.capture.queue = []CapturedRequest{{Method: "POST", URL: "/login", Status: 401}}

	yaml := "appId: com.test\nproxy: required\n---\n" +
		"- capture: \"POST /login as: loginRsp\"\n" +
		"- branch:\n" +
		"    from: loginRsp\n" +
		"    on:\n" +
		"      - match:\n" +
		"          status: 200\n" +
		"        do:\n" +
		"          - waitFor: Dashboard\n" +
		"      - match:\n" +
		"          status: 401\n" +
		"        do:\n" +
		"          - waitFor: Dashboard\n"
	result := run(t, tr, yaml)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
}

func TestBranchCommand_NoMatchNoDefaultFails(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{Method: "POST", URL: "/login", Status: 500}}

	yaml := "appId: com.test\nproxy: required\n---\n" +
		"- capture: \"POST /login as: r\"\n" +
		"- branch:\n" +
		"    from: r\n" +
		"    on:\n" +
		"      - match:\n" +
		"          status: 200\n" +
		"        do:\n" +
		"          - back\n"
	result := run(t, tr, yaml)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "branch: no case matched for \"r\" (status=500)"
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestReadCommand_ReadsAttribute(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(wire.TreeNodeWire{
		Attributes: map[string]string{"id": "field1", "value": "hello", "bounds": "0,0,10,10"},
	}))

	yaml := "appId: com.test\n---\n- read:\n    selector:\n      id: field1\n    field: value\n    as: v\n"
	result := run(t, tr, yaml)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != `Read value into "v"` {
		t.Fatalf("got detail %q", got)
	}
}

const capturedSecret = "tok-s3cr3t-9f3a"

func runWithCapturedToken(t *testing.T, steps string) capturedRun {
	t.Helper()
	return runWithCapturedBody(t, map[string]any{"token": capturedSecret, "profile": map[string]any{"a": "1", "b": "2"}}, steps)
}

func runWithCapturedBody(t *testing.T, body map[string]any, steps string) capturedRun {
	t.Helper()
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{Method: "POST", URL: "/login", Status: 200, Body: body}}
	result := run(t, tr, "appId: com.test\nproxy: required\n---\n- capture: \"POST /login as: r\"\n"+steps)
	return capturedRun{result: result, last: detail(lastStep(t, result))}
}

// capturedRun carries a run result and the detail of its last step.
type capturedRun struct {
	result ScriptResult
	last   string
}

func TestExtractCommand_DetailNamesVariablesAndLengthsOnly(t *testing.T) {
	view := runWithCapturedToken(t, "- extract:\n    from: r\n    values:\n      token: $.body.token\n")

	if !view.result.Ok {
		t.Fatalf("extract failed: %s", view.last)
	}
	if want := "Extracted: token (15 characters)"; view.last != want {
		t.Fatalf("detail = %q, want %q", view.last, want)
	}
	if strings.Contains(view.last, capturedSecret) {
		t.Fatalf("detail carries the extracted value: %q", view.last)
	}
}

func TestAssertApiCommand_FailureNeverPrintsTheCapturedString(t *testing.T) {
	view := runWithCapturedToken(t, "- assertApi:\n    from: r\n    body:\n      $.token: something-else\n      $.profile: $contains:x\n")

	if view.result.Ok {
		t.Fatal("assertion unexpectedly passed")
	}
	if strings.Contains(view.last, capturedSecret) {
		t.Fatalf("detail carries the captured token: %q", view.last)
	}
	if !strings.Contains(view.last, "got a string of 15 characters") {
		t.Fatalf("detail = %q, want the captured string described by length", view.last)
	}
}

func TestAssertExprCommand_NonBooleanFailureNeverPrintsTheValue(t *testing.T) {
	view := runWithCapturedToken(t, "- extract:\n    from: r\n    values:\n      token: $.body.token\n- assertExpr:\n    expr: token\n")

	if view.result.Ok {
		t.Fatal("assertion unexpectedly passed")
	}
	if strings.Contains(view.last, capturedSecret) {
		t.Fatalf("detail carries the variable value: %q", view.last)
	}
	if want := `Assertion "token" must evaluate to a boolean, got string`; view.last != want {
		t.Fatalf("detail = %q, want %q", view.last, want)
	}
}

func TestAssertExprCommand_ConversionFailureNeverPrintsTheValue(t *testing.T) {
	for _, expr := range []string{"number(token) > 1", "bool(token)", "token + 1", "token < 1"} {
		view := runWithCapturedToken(t, "- extract:\n    from: r\n    values:\n      token: $.body.token\n- assertExpr:\n    expr: "+expr+"\n")

		if view.result.Ok {
			t.Fatalf("%q unexpectedly passed", expr)
		}
		if strings.Contains(view.last, capturedSecret) {
			t.Fatalf("%q: detail carries the variable value: %q", expr, view.last)
		}
	}
}

func TestAssertApiCommand_NumericFailureNeverPrintsTheCapturedNumber(t *testing.T) {
	const balance = 48151623
	body := map[string]any{"balance": float64(balance)}
	for _, check := range []string{"$between:1,2", "$gt:999999999", "$lt:1", "$not_exists", "other"} {
		view := runWithCapturedBody(t, body, "- assertApi:\n    from: r\n    body:\n      $.balance: \""+check+"\"\n")

		if view.result.Ok {
			t.Fatalf("%q unexpectedly passed", check)
		}
		if strings.Contains(view.last, "48151623") {
			t.Fatalf("%q: detail carries the captured number: %q", check, view.last)
		}
	}
}
