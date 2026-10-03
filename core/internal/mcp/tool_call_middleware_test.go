package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestValueConstraintRejectsOutOfRangeArgument exercises the central
// value-constraint validation the published (loose) schemas don't
// enforce: get_ui_tree's `limit` is declared `z.number().int().positive()`
// on the TypeScript server, so a negative limit must be rejected with
// the same "Invalid arguments" prefix the TypeScript server's Zod
// safeParse failure produces, mirroring server.test.ts's
// prefix-only assertion rather than any specific message body.
func TestValueConstraintRejectsOutOfRangeArgument(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, _ := callTool(t, cs, ctx, "get_ui_tree", map[string]any{"limit": float64(-1)})
	if !result.IsError {
		t.Fatal("get_ui_tree(limit:-1): expected isError true")
	}
	if got := textOf(t, result); !containsInvalidArguments(got) {
		t.Fatalf("get_ui_tree(limit:-1) text: got %q, want prefix %q", got, "Invalid arguments")
	}
}

// TestValueConstraintRejectsBlankRequiredString covers the string-side
// of the same gap: start_run's `name` is `z.string().min(1)` on the
// TypeScript server, so an empty string (present, but blank) must be
// rejected the same way a missing field is.
func TestValueConstraintRejectsBlankRequiredString(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	result, _ := callTool(t, cs, ctx, "start_run", map[string]any{"name": ""})
	if !result.IsError {
		t.Fatal("start_run(name:\"\"): expected isError true")
	}
	if got := textOf(t, result); !containsInvalidArguments(got) {
		t.Fatalf("start_run(name:\"\") text: got %q, want prefix %q", got, "Invalid arguments")
	}
}

// TestToolTimeoutReturnsStructuredPayload exercises the central
// per-call timeout wrapper: a hanging driver call must be aborted once
// the wrapper's budget elapses, with the wrapper returning the
// structured TOOL_TIMEOUT payload rather than blocking forever or
// surfacing a raw context error. ATOMYX_TOOL_TIMEOUT_MS is set to a
// small value (mirroring server.test.ts's own "150"-style override)
// so the test observes a real but tiny wall-clock timeout instead of
// waiting on the 30s default.
func TestToolTimeoutReturnsStructuredPayload(t *testing.T) {
	t.Setenv(toolTimeoutEnvVar, "50")

	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	drv.HangOnNextHierarchy = true
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "get_ui_tree", map[string]any{})
	if !result.IsError {
		t.Fatalf("get_ui_tree (hung driver): expected isError true, got %v", decoded)
	}
	if decoded["code"] != "TOOL_TIMEOUT" {
		t.Fatalf("get_ui_tree (hung driver) code: got %v, want TOOL_TIMEOUT", decoded["code"])
	}
	if decoded["tool"] != "get_ui_tree" {
		t.Fatalf("get_ui_tree (hung driver) tool: got %v, want get_ui_tree", decoded["tool"])
	}
	if _, ok := decoded["elapsedMs"].(float64); !ok {
		t.Fatalf("get_ui_tree (hung driver) elapsedMs: got %v, want number", decoded["elapsedMs"])
	}
	if _, ok := decoded["budgetMs"].(float64); !ok {
		t.Fatalf("get_ui_tree (hung driver) budgetMs: got %v, want number", decoded["budgetMs"])
	}
	if v, present := decoded["declaredBudgetMs"]; !present || v != nil {
		t.Fatalf("get_ui_tree (hung driver) declaredBudgetMs: got %v, want explicit null", v)
	}
	hint, _ := decoded["hint"].(string)
	if hint != toolTimeoutHint {
		t.Fatalf("get_ui_tree (hung driver) hint: got %q, want the TypeScript-server hint verbatim", hint)
	}

	// A fresh call after the hang must succeed normally: the wrapper's
	// deadline for one call must not leak into the next.
	result2, decoded2 := callTool(t, cs, ctx, "get_ui_tree", map[string]any{})
	if result2.IsError {
		t.Fatalf("get_ui_tree after timeout: unexpected isError: %v", decoded2)
	}
	if _, ok := decoded2["nodes"].([]any); !ok {
		t.Fatalf("get_ui_tree after timeout: got %v, want a nodes array", decoded2)
	}
}

// TestResolveToolTimeoutBudget exercises the budget arithmetic
// directly (no server, no waiting) so the declared-budget-plus-buffer
// and default-floor paths are covered deterministically. Driving this
// through an actual hung call would require the real 5-second buffer
// to elapse, which resolveToolTimeoutBudget's own math already
// determines without needing a live timeout to prove it.
func TestResolveToolTimeoutBudget(t *testing.T) {
	t.Setenv(toolTimeoutEnvVar, "1000")

	// No declared timeoutMs/maxTimeoutMs: falls back to the env
	// default.
	budgetMs, declared := resolveToolTimeoutBudget([]byte(`{}`))
	if budgetMs != 1000 || declared != nil {
		t.Fatalf("resolveToolTimeoutBudget({}): got (%v, %v), want (1000, nil)", budgetMs, declared)
	}

	// Declared timeoutMs + buffer exceeds the default: the declared
	// budget (plus buffer) wins.
	budgetMs, declared = resolveToolTimeoutBudget([]byte(`{"timeoutMs":2000}`))
	if budgetMs != 2000+toolTimeoutBufferMs || declared == nil || *declared != 2000 {
		t.Fatalf("resolveToolTimeoutBudget(timeoutMs:2000): got (%v, %v), want (%v, 2000)",
			budgetMs, declared, 2000+toolTimeoutBufferMs)
	}

	// Declared maxTimeoutMs + buffer is still below a larger env
	// default: the default floors it.
	t.Setenv(toolTimeoutEnvVar, "10000")
	budgetMs, declared = resolveToolTimeoutBudget([]byte(`{"maxTimeoutMs":10}`))
	if budgetMs != 10000 || declared == nil || *declared != 10 {
		t.Fatalf("resolveToolTimeoutBudget(maxTimeoutMs:10): got (%v, %v), want (10000, 10)", budgetMs, declared)
	}
	t.Setenv(toolTimeoutEnvVar, "1000")

	// maxTimeoutMs takes precedence over timeoutMs when both are
	// present, matching the TypeScript server's precedence.
	budgetMs, declared = resolveToolTimeoutBudget([]byte(`{"timeoutMs":50,"maxTimeoutMs":2000}`))
	if budgetMs != 2000+toolTimeoutBufferMs || declared == nil || *declared != 2000 {
		t.Fatalf("resolveToolTimeoutBudget(both set): got (%v, %v), want (%v, 2000)",
			budgetMs, declared, 2000+toolTimeoutBufferMs)
	}
}

func containsInvalidArguments(s string) bool {
	return strings.Contains(s, "Invalid arguments")
}

type interventionsBody struct {
	Interventions []session.Intervention `json:"interventions"`
}

// interventionHandler is the middleware over a handler that returns one
// content block, so any further block is something the middleware added.
func interventionHandler(f *lifecycleFixture, observeOnly ...string) func(tool string) *mcp.CallToolResult {
	traits := newToolTraits()
	for _, name := range observeOnly {
		traits.observeOnly[name] = true
	}
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, f.clk, f.devices, f.devices, traits)(next)
	return func(tool string) *mcp.CallToolResult {
		f.t.Helper()
		res, err := handler(context.Background(), toolCallMethod, toolCallRequest(tool))
		if err != nil {
			f.t.Fatalf("%s: %v", tool, err)
		}
		return res.(*mcp.CallToolResult)
	}
}

// interventionsOf returns the entries a result carries, failing when it
// has more than one extra block or a block that is not the entries.
func interventionsOf(t *testing.T, res *mcp.CallToolResult) []session.Intervention {
	t.Helper()
	if len(res.Content) == 1 {
		return nil
	}
	if len(res.Content) != 2 {
		t.Fatalf("result has %d blocks, want the tool's own block and at most one more", len(res.Content))
	}
	var body interventionsBody
	text := res.Content[1].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("extra block %q is not the interventions body: %v", text, err)
	}
	if len(body.Interventions) == 0 {
		t.Fatalf("extra block %q carries no entries, want no block at all", text)
	}
	return body.Interventions
}

func TestToolCallMiddleware_AResultCarriesInterventionsOnlyWhenNewOnesExist(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	call := interventionHandler(f)

	if got := interventionsOf(t, call("tap")); got != nil {
		t.Fatalf("interventions = %+v before any viewer attached, want none", got)
	}
	f.viewer("atomyx inspect")
	got := interventionsOf(t, call("tap"))
	if len(got) != 1 || got[0].Kind != session.InterventionViewerAttached || got[0].Client != "atomyx inspect" || got[0].AtMs == 0 {
		t.Fatalf("interventions = %+v, want one viewerAttached from atomyx inspect with a time", got)
	}
	if again := interventionsOf(t, call("tap")); again != nil {
		t.Fatalf("interventions = %+v on the next call, want none because they were drained", again)
	}
}

func TestAttachInterventions_TheBlockCarriesKindAtMsAndClient(t *testing.T) {
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}
	attachInterventions(res, []session.Intervention{
		{Kind: session.InterventionViewerAttached, AtMs: 5, Client: "a"},
		{Kind: session.InterventionViewerDetached, AtMs: 9, Client: "a"},
	})
	want := `{"interventions":[{"kind":"viewerAttached","atMs":5,"client":"a"},{"kind":"viewerDetached","atMs":9,"client":"a"}]}`
	if got := res.Content[1].(*mcp.TextContent).Text; got != want {
		t.Fatalf("block = %s, want %s", got, want)
	}
}

func TestToolCallMiddleware_ObserveOnlyToolsStillCarryInterventionsWithoutCountingAsActivity(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.viewer("atomyx inspect")
	call := interventionHandler(f, "check_health", "list_devices", "disconnect_device", "select_device")

	for _, tool := range []string{"check_health", "list_devices"} {
		f.viewer("atomyx inspect")
		if got := interventionsOf(t, call(tool)); len(got) == 0 {
			t.Fatalf("%s carried no interventions, want the pending viewer", tool)
		}
	}
	f.devices.mu.Lock()
	inFlight := f.devices.inFlight
	f.devices.mu.Unlock()
	if inFlight != 0 {
		t.Fatalf("inFlight = %d after observe-only calls, want 0", inFlight)
	}
}

func TestDeviceSession_AnIdleReleaseThenReacquireLosesNoIntervention(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.viewer("before release")
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)
	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.viewer("after reacquire")

	requireSummary(t, f.devices.Interventions(), "viewerAttached:before release", "viewerDetached:before release", "viewerAttached:after reacquire")
}

func TestDeviceSession_AReleasedSessionStillReportsWhatHappenedBeforeTheRelease(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.viewer("before release")
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	requireSummary(t, f.devices.Interventions(), "viewerAttached:before release", "viewerDetached:before release")
}

func TestDeviceSession_ALostLeaseThenReacquireLosesNoIntervention(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.viewer("before loss")
	lockPath := f.leases.LockPathFor("android", "A")
	replacement := filepath.Join(filepath.Dir(lockPath), "replacement")
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, lockPath); err != nil {
		t.Fatal(err)
	}
	intruder, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(intruder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	f.leases.FireRecheck()
	f.awaitRelease(f.devices)
	_ = intruder.Close()
	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	requireSummary(t, f.devices.Interventions(), "viewerAttached:before loss", "viewerDetached:before loss")
}

func TestDeviceSession_SelectingAnotherDeviceKeepsTheOutgoingInterventions(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.viewer("on the first device")

	if _, err := f.devices.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformAndroid, ID: "B"}); err != nil {
		t.Fatal(err)
	}

	requireSummary(t, f.devices.Interventions(), "viewerAttached:on the first device", "viewerDetached:on the first device")
}

func TestDeviceSession_ADisconnectKeepsTheOutgoingInterventions(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.viewer("before disconnect")

	if err := f.devices.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	requireSummary(t, f.devices.Interventions(), "viewerAttached:before disconnect", "viewerDetached:before disconnect")
}

func summarize(entries []session.Intervention) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Kind + ":" + e.Client
	}
	return out
}

func requireSummary(t *testing.T, got []session.Intervention, want ...string) {
	t.Helper()
	if s := summarize(got); !reflect.DeepEqual(s, want) {
		t.Fatalf("interventions = %v, want %v", s, want)
	}
}
