package mcp

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const gateSecret = "s3cr3t-sentinel-9f3a"

// gateScreen is a login screen whose password field holds the sentinel
// in both its text and its value.
func gateScreen(extra ...wire.TreeNodeWire) wire.TreeNodeWire {
	enabled, clickable, focused := true, true, true
	password := wire.TreeNodeWire{
		Attributes: map[string]string{
			"role": "text-field", "id": "pw", "label": "Password", "hint": "Enter password",
			"text": gateSecret, "value": gateSecret, "secure": "true", "bounds": "0,200,100,250",
		},
		Enabled: &enabled, Clickable: &clickable, Focused: &focused,
	}
	return wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children:   append([]wire.TreeNodeWire{loginButtonNode(), password}, extra...),
	}
}

// gateCall is one tool invocation of the gate. mustSucceed marks a
// tool that returns tree or element data, so a call that fails on its
// arguments cannot make the gate pass without testing anything.
type gateCall struct {
	args        map[string]any
	mustSucceed bool
	// before, when set, is the screen the first reads see, so a tool
	// that diffs two screens finds the password field as new.
	before *wire.TreeNodeWire
}

// gateCalls lists every tool the server registers. A tool missing from
// the table fails the gate, so a new tool has to be checked against the
// secure fixture before it ships.
func gateCalls() map[string]gateCall {
	selector := map[string]any{"id": "pw"}
	loginOnly := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children:   []wire.TreeNodeWire{loginButtonNode()},
	}
	script := "appId: com.example.app\nname: Login\nsteps:\n  - type:\n      into:\n        id: pw\n      text: " + gateSecret + "\n"
	return map[string]gateCall{
		"list_devices":      {args: map[string]any{}},
		"select_device":     {args: map[string]any{"platform": "android", "id": "emulator-5554"}, mustSucceed: true},
		"disconnect_device": {args: map[string]any{}},
		"check_health":      {args: map[string]any{}},
		"list_apps":         {args: map[string]any{}},
		"launch_app":        {args: map[string]any{"appId": "com.example.app"}, mustSucceed: true},
		"get_ui_tree":       {args: map[string]any{}, mustSucceed: true},
		"find_element":      {args: selector, mustSucceed: true},
		"screenshot":        {args: map[string]any{}},
		"tap":               {args: map[string]any{"selector": selector}, mustSucceed: true},
		"tap_and_wait_transition": {
			args: map[string]any{
				"selector": map[string]any{"id": "login-button"}, "waitForAppear": map[string]any{"id": "never-shown"},
				"timeoutMs": float64(100), "maxTimeoutMs": float64(100), "intervalMs": float64(50),
			},
			mustSucceed: true, before: &loginOnly,
		},
		"input_text":         {args: map[string]any{"selector": selector, "text": gateSecret}, mustSucceed: true},
		"swipe":              {args: map[string]any{"direction": "up"}, mustSucceed: true},
		"press_key":          {args: map[string]any{"key": "back"}, mustSucceed: true},
		"wait_for_element":   {args: map[string]any{"selector": selector, "timeoutMs": float64(100)}, mustSucceed: true},
		"start_run":          {args: map[string]any{"name": "gate"}},
		"finish_run":         {args: map[string]any{"status": "passed"}},
		"report_bug":         {args: map[string]any{"title": "t", "description": "d"}},
		"list_runs":          {args: map[string]any{}},
		"get_test_stats":     {args: map[string]any{}},
		"get_run":            {args: map[string]any{"id": "missing"}},
		"list_bugs":          {args: map[string]any{}},
		"get_bug":            {args: map[string]any{"id": "missing"}},
		"update_run_summary": {args: map[string]any{"id": "missing", "summary": "s"}},
		"delete_run":         {args: map[string]any{"id": "missing"}},
		"delete_bug":         {args: map[string]any{"id": "missing"}},
		"add_case_study":     {args: map[string]any{"title": "t", "trigger": "x", "solution": "y"}},
		"get_case_studies":   {args: map[string]any{}},
		"run_script":         {args: map[string]any{"script": script}, mustSucceed: true},
	}
}

// TestEverySecureElementIsMaskedInEveryToolResult calls every
// registered tool against a screen with a secure password field and
// asserts the secret appears in no result. It fails when a tool leaks
// the value, when a tool skips the result masking, and when a new tool
// is not yet part of the table.
func TestEverySecureElementIsMaskedInEveryToolResult(t *testing.T) {
	calls := gateCalls()
	probe := mockDeviceServer(t, gateScreen(), nil)
	registered, err := probe.cs.ListTools(probe.ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range registered.Tools {
		if _, ok := calls[tool.Name]; !ok {
			t.Errorf("tool %s is registered but missing from gateCalls", tool.Name)
		}
	}
	if len(registered.Tools) != len(calls) {
		t.Errorf("registered %d tools, gate lists %d", len(registered.Tools), len(calls))
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			env := mockDeviceServer(t, gateScreen(), call.before)
			if name != "select_device" {
				selectMockDevice(t, env.cs, env.ctx)
			}
			result, err := env.cs.CallTool(env.ctx, &mcp.CallToolParams{Name: name, Arguments: call.args})
			if err != nil {
				t.Fatalf("CallTool(%s): %v", name, err)
			}
			if call.mustSucceed && result.IsError {
				t.Fatalf("%s failed, so the gate proves nothing: %s", name, textOf(t, result))
			}
			for _, content := range result.Content {
				if text, ok := content.(*mcp.TextContent); ok && strings.Contains(text.Text, gateSecret) {
					t.Fatalf("%s result carries the secret:\n%s", name, text.Text)
				}
			}
		})
	}
}

func TestGetUiTreeMasksSecureValuesAndKeepsTheLabel(t *testing.T) {
	env := mockDeviceServer(t, gateScreen(), nil)
	selectMockDevice(t, env.cs, env.ctx)

	_, decoded := callTool(t, env.cs, env.ctx, "get_ui_tree", map[string]any{})

	var password map[string]any
	for _, n := range decoded["nodes"].([]any) {
		if node := n.(map[string]any); node["id"] == "pw" {
			password = node
		}
	}
	if password == nil {
		t.Fatalf("password node missing from %v", decoded)
	}
	if password["text"] != orchestra.SecureMask || password["value"] != orchestra.SecureMask || password["secure"] != true {
		t.Fatalf("password node = %v, want masked text and value and secure true", password)
	}
	if password["label"] != "Password" || password["hint"] != "Enter password" || password["role"] != "text-field" {
		t.Fatalf("password node = %v, want label, hint and role kept", password)
	}
}

func TestFindElementStillResolvesASecureFieldByItsRealValue(t *testing.T) {
	env := mockDeviceServer(t, gateScreen(), nil)
	selectMockDevice(t, env.cs, env.ctx)

	_, decoded := callTool(t, env.cs, env.ctx, "find_element", map[string]any{"text": gateSecret})

	if decoded["found"] != true || decoded["text"] != orchestra.SecureMask || decoded["secure"] != true {
		t.Fatalf("find_element = %v, want a match on the real value reported masked", decoded)
	}
}

type mockDeviceEnv struct {
	cs  *mcp.ClientSession
	ctx context.Context
}

// mockDeviceServer starts a server over a mock device that shows
// screen. When before is set, the first reads see before instead.
func mockDeviceServer(t *testing.T, screen wire.TreeNodeWire, before *wire.TreeNodeWire) mockDeviceEnv {
	t.Helper()
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	if before != nil {
		drv.StageHierarchyRepeated(*before, 5)
	}
	drv.StageHierarchy(screen)
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: clockedMockDriverFactory{driver: drv, clock: orchestra.NewFakeClock(0)},
		Storage: storage.NewInMemoryStorage(),
	})
	return mockDeviceEnv{cs: cs, ctx: ctx}
}
