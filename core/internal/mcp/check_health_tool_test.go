package mcp

import (
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestCheckHealth_NoActiveDeviceReportsSelectedFalseWithoutError(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: device.NewMockDriverFactory(),
		Storage: storage.NewInMemoryStorage(),
	})

	result, decoded := callTool(t, cs, ctx, "check_health", map[string]any{})
	if result.IsError {
		t.Fatalf("check_health with no active device: unexpected isError: %v", decoded)
	}
	if decoded["selected"] != false {
		t.Fatalf("check_health selected: got %v, want false", decoded["selected"])
	}
	if decoded["healthy"] != false {
		t.Fatalf("check_health healthy: got %v, want false alongside selected:false", decoded["healthy"])
	}
	if _, present := decoded["code"]; present {
		t.Fatalf("check_health: unexpected code field with no device selected: %v", decoded)
	}
}

func TestCheckHealth_HealthyDeviceReportsSelectedAndHealthyTrue(t *testing.T) {
	drv := mockdriver.New()
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "check_health", map[string]any{})
	if result.IsError {
		t.Fatalf("check_health (healthy): unexpected isError: %v", decoded)
	}
	if decoded["selected"] != true {
		t.Fatalf("check_health selected: got %v, want true", decoded["selected"])
	}
	if decoded["healthy"] != true {
		t.Fatalf("check_health healthy: got %v, want true", decoded["healthy"])
	}
}

func TestCheckHealth_UnreachableAgentReportsHealthyFalseWithoutIsError(t *testing.T) {
	drv := mockdriver.New()
	drv.PingErr = diag.New(
		diag.CodeAgentUnreachable,
		"could not reach the agent on device emulator-5554",
		"Confirm the agent is running on the device, then try again.",
		nil,
	)
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "check_health", map[string]any{})
	if result.IsError {
		t.Fatalf("check_health (unreachable): expected isError false for a successfully-checked but unreachable device, got true: %v", decoded)
	}
	if decoded["selected"] != true {
		t.Fatalf("check_health selected: got %v, want true", decoded["selected"])
	}
	if decoded["healthy"] != false {
		t.Fatalf("check_health healthy: got %v, want false", decoded["healthy"])
	}
	if decoded["code"] != diag.CodeAgentUnreachable {
		t.Fatalf("check_health code: got %v, want %s", decoded["code"], diag.CodeAgentUnreachable)
	}
	if hint, _ := decoded["remedy"].(string); hint == "" {
		t.Fatalf("check_health remedy: got %v, want a non-empty remedy", decoded["remedy"])
	}
}

func TestCheckHealth_ReportsAgentVersionsOnlyWhenTheAgentReportsThem(t *testing.T) {
	drv := mockdriver.New()
	drv.Agent = driver.AgentInfo{ProtocolVersion: 3, AgentVersion: "0.1.0"}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	_, decoded := callTool(t, cs, ctx, "check_health", map[string]any{})
	if decoded["agentVersion"] != "0.1.0" || decoded["protocolVersion"] != float64(3) {
		t.Fatalf("check_health = %v, want agentVersion 0.1.0 and protocolVersion 3", decoded)
	}

	bare, bareCtx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, bare, bareCtx)
	_, decoded = callTool(t, bare, bareCtx, "check_health", map[string]any{})
	for _, key := range []string{"agentVersion", "protocolVersion"} {
		if _, present := decoded[key]; present {
			t.Fatalf("check_health carries %s for an agent that reports none: %v", key, decoded)
		}
	}
}
