package mcp

import (
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
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
