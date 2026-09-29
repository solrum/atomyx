package mcp

import (
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/wire"
)

func TestInputTextSelectorDispatchesToDriver(t *testing.T) {
	drv := mockdriver.New()
	// The target field reports focus so the input-focus gate resolves
	// and the driver receives the typed text.
	focused := true
	drv.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children: []wire.TreeNodeWire{{
			Attributes: map[string]string{
				"role":   "button",
				"id":     "login-button",
				"text":   "Log in",
				"bounds": "0,100,100,150",
			},
			Focused: &focused,
		}},
	})
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "input_text", map[string]any{
		"selector": map[string]any{"id": "login-button"},
		"text":     "hello",
	})
	if result.IsError {
		t.Fatalf("input_text(selector): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true {
		t.Fatalf("input_text(selector): got %v, want ok:true", decoded)
	}
	foundInputText := false
	for _, c := range drv.Calls {
		if c.Method == "InputText" {
			foundInputText = true
			if len(c.Args) == 0 || c.Args[0] != "hello" {
				t.Fatalf("input_text(selector) driver call args: got %v, want [\"hello\"]", c.Args)
			}
		}
	}
	if !foundInputText {
		t.Fatal("input_text(selector): driver did not record an InputText call")
	}
}
