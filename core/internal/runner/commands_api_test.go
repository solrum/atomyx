package runner

import (
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
	want := "API assertion failed on \"r\":\n  - body $.name: expected $not_empty, got \"\""
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
