package mcp

import (
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func publishingServer(t *testing.T) (*Server, runServer) {
	t.Helper()
	root := t.TempDir()
	drv := mockdriver.New()
	drv.StageHierarchy(sampleTree())
	server, cs, ctx := testServerHandle(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    singleMockDriverFactory{driver: drv},
		Storage:    storage.NewInMemoryStorage(),
		RunStore:   storage.NewRunStore(),
		RunStarter: testRunStarter(root),
	})
	return server, runServer{root: root, cs: cs, call: func(name string, args map[string]any) map[string]any {
		_, decoded := callTool(t, cs, ctx, name, args)
		return decoded
	}}
}

func TestRunScriptPublishesItsStepLinesToTheHeldSession(t *testing.T) {
	server, r := publishingServer(t)
	selectMockDevice(t, r.cs, t.Context())

	if got := r.call("run_script", map[string]any{"script": passingScreenshotScript}); got["ok"] != true {
		t.Fatalf("run_script = %v", got)
	}

	server.session.mu.Lock()
	held := server.session.sess
	server.session.mu.Unlock()
	lines, gap := held.StepsSince(0)
	if gap || len(lines) != 2 || lines[0].Seq != 1 || lines[1].Seq != 2 || lines[1].Command != "screenshot" {
		t.Fatalf("held session lines = %+v gap %v, want the two stamped lines of the script", lines, gap)
	}
}

func TestRunScriptInAnOpenRunPublishesItsStepLinesToTheHeldSession(t *testing.T) {
	server, r := publishingServer(t)
	selectMockDevice(t, r.cs, t.Context())
	r.call("start_run", map[string]any{"name": "explore"})

	if got := r.call("run_script", map[string]any{"script": passingScreenshotScript}); got["ok"] != true {
		t.Fatalf("run_script = %v", got)
	}

	server.session.mu.Lock()
	held := server.session.sess
	server.session.mu.Unlock()
	if lines, _ := held.StepsSince(0); len(lines) != 2 {
		t.Fatalf("held session has %d lines, want 2", len(lines))
	}
}
