package mcp

import (
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/wire"
)

func TestToolCallsPublishTheirCapturesToTheSessionHubThroughTheOrchestra(t *testing.T) {
	drv := mockdriver.New()
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "home-root"}})
	server, cs, ctx := testServerHandle(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})
	held := server.session.sess
	if held == nil {
		t.Fatal("select_device left no held session")
	}
	if held.Latest().HasTree {
		t.Fatal("the hub held a tree before any tree was read")
	}

	result, _ := callTool(t, cs, ctx, "get_ui_tree", map[string]any{})
	if result.IsError {
		t.Fatalf("get_ui_tree failed: %s", textOf(t, result))
	}

	snap := held.Latest()
	if !snap.HasTree || snap.Tree.Attributes["id"] != "home-root" || snap.TreeRevision == 0 {
		t.Fatalf("hub tree = %+v after get_ui_tree, want the tree the handler read", snap)
	}
}
