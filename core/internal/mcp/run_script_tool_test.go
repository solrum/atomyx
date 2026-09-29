package mcp

import (
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestRunScriptStepFailureReportsFailedAtStep(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	script := "appId: com.example.app\nname: Smoke\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
	if result.IsError {
		t.Fatalf("run_script (step failure): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("run_script (step failure): got %v, want ok:false", decoded)
	}
	if decoded["failedAtStep"] != float64(0) {
		t.Fatalf("run_script (step failure) failedAtStep: got %v, want 0", decoded["failedAtStep"])
	}
	steps, ok := decoded["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("run_script (step failure) steps: got %v, want a single entry", decoded["steps"])
	}
	step0 := steps[0].(map[string]any)
	if step0["ok"] != false {
		t.Fatalf("run_script (step failure) steps[0]: got %v, want ok:false", step0)
	}
	if _, present := step0["deviceLost"]; present {
		t.Fatalf("run_script (step failure) steps[0]: got deviceLost present as %v, want omitted for an ordinary failure", step0["deviceLost"])
	}
	if _, present := decoded["deviceLost"]; present {
		t.Fatalf("run_script (step failure): got top-level deviceLost present as %v, want omitted for an ordinary failure", decoded["deviceLost"])
	}
}

// unreachableAgentError models a driver liveness failure the same way
// a real driver's coded error does, so the liveness probe run_script
// triggers after a failed step classifies it as the device having
// gone away rather than an ordinary command failure.
type unreachableAgentError struct{}

func (unreachableAgentError) Error() string  { return "could not reach the agent on the device" }
func (unreachableAgentError) Code() string   { return "AGENT_UNREACHABLE" }
func (unreachableAgentError) Remedy() string { return "Confirm the agent is running, then try again." }

func TestRunScriptDeviceLostFlagsFailedStepAndTopLevel(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	drv.FailNextPingForContract(unreachableAgentError{})
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	script := "appId: com.example.app\nname: Smoke\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"
	result, decoded := callTool(t, cs, ctx, "run_script", map[string]any{"script": script})
	if result.IsError {
		t.Fatalf("run_script (device lost): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("run_script (device lost): got %v, want ok:false", decoded)
	}
	if decoded["deviceLost"] != true {
		t.Fatalf("run_script (device lost): got deviceLost=%v, want true", decoded["deviceLost"])
	}
	steps, ok := decoded["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("run_script (device lost) steps: got %v, want a single entry", decoded["steps"])
	}
	step0 := steps[0].(map[string]any)
	if step0["deviceLost"] != true {
		t.Fatalf("run_script (device lost) steps[0]: got deviceLost=%v, want true", step0["deviceLost"])
	}
}
