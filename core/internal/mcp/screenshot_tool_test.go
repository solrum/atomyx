package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

func TestScreenshotWithNoRunSavesUnderTheSharedScreenshotsDirectory(t *testing.T) {
	r := newRunServer(t)
	selectMockDevice(t, r.cs, t.Context())

	decoded := r.call("screenshot", map[string]any{})
	path, _ := decoded["path"].(string)
	if path == "" || decoded["format"] != "png" {
		t.Fatalf("screenshot = %v", decoded)
	}
	if filepath.Dir(path) != runfolder.LooseScreenshotsDir(r.root) {
		t.Fatalf("path %q, want a file directly under %q", path, runfolder.LooseScreenshotsDir(r.root))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("screenshot file not written: %v", err)
	}
	if metas, _, _ := runfolder.NewReader(r.root).List(); len(metas) != 0 {
		t.Fatalf("a screenshot with no run open created %d run folders", len(metas))
	}
}

func TestScreenshotInsideARunLandsInItsArtifactsAndFinishFinalizesIt(t *testing.T) {
	r := newRunServer(t)
	selectMockDevice(t, r.cs, t.Context())
	id := r.call("start_run", map[string]any{"name": "smoke"})["id"].(string)

	path, _ := r.call("screenshot", map[string]any{})["path"].(string)
	if filepath.Dir(path) != runfolder.ArtifactsDir(runfolder.RunDir(r.root, id)) {
		t.Fatalf("path %q, want a file in the run's artifacts directory", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("screenshot file not written: %v", err)
	}
	if _, err := os.Stat(runfolder.LooseScreenshotsDir(r.root)); !os.IsNotExist(err) {
		t.Fatalf("a screenshot inside a run created the shared directory: %v", err)
	}

	r.call("finish_run", map[string]any{})
	meta := r.onlyMeta(t)
	if meta.Outcome != runfolder.OutcomePassed || meta.CompletedAt == nil {
		t.Fatalf("meta = %+v, want a finalized passed run", meta)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("screenshot missing after finish: %v", err)
	}
}

func TestScreenshotSurfacesDriverError(t *testing.T) {
	drv := mockdriver.New()
	drv.ScreenshotErr = errors.New("capture failed")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "screenshot", map[string]any{})
	if !result.IsError {
		t.Fatal("screenshot (driver error): expected isError true")
	}
	if decoded["code"] != diag.CodeUnknown {
		t.Fatalf("screenshot (driver error) code: got %v, want %s", decoded["code"], diag.CodeUnknown)
	}
	if decoded["message"] != "capture failed" {
		t.Fatalf("screenshot (driver error) message: got %v, want %q", decoded["message"], "capture failed")
	}
}

func TestScreenshotPathsDeriveFromTheRootOfTheRunStarter(t *testing.T) {
	root := t.TempDir()
	drv := mockdriver.New()
	cs, ctx := testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    singleMockDriverFactory{driver: drv},
		Storage:    storage.NewInMemoryStorage(),
		RunStarter: testRunStarter(root),
	})
	selectMockDevice(t, cs, ctx)
	shot := func() string {
		_, decoded := callTool(t, cs, ctx, "screenshot", map[string]any{})
		path, _ := decoded["path"].(string)
		return path
	}

	loose := shot()
	callTool(t, cs, ctx, "start_run", map[string]any{"name": "smoke"})
	inRun := shot()

	for _, path := range []string{loose, inRun} {
		if !filepath.IsAbs(path) || !strings.HasPrefix(path, root) {
			t.Fatalf("screenshot path %q, want a path under the starter root %q", path, root)
		}
	}
}
