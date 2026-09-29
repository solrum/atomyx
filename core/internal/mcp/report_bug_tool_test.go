package mcp

import (
	"encoding/base64"
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestReportBugCapturesScreenshotWhenDeviceActive(t *testing.T) {
	drv := mockdriver.New()
	strg := storage.NewInMemoryStorage()
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: strg,
	})
	selectMockDevice(t, cs, ctx)

	_, started := callTool(t, cs, ctx, "start_run", map[string]any{"name": "screenshot capture"})
	if started["ok"] != true {
		t.Fatalf("start_run: got %v", started)
	}

	_, bug := callTool(t, cs, ctx, "report_bug", map[string]any{
		"title":       "Crash on submit",
		"description": "App crashes when tapping submit.",
	})
	if bug["ok"] != true {
		t.Fatalf("report_bug: got %v", bug)
	}
	screenshotPath, ok := bug["screenshotPath"].(string)
	if !ok || screenshotPath == "" {
		t.Fatalf("report_bug: expected a screenshotPath with an active device, got %v", bug)
	}

	var record screenshotRecord
	found, err := strg.Load(screenshotPath, &record)
	if err != nil || !found {
		t.Fatalf("report_bug: screenshot record not persisted at %q: found=%v err=%v", screenshotPath, found, err)
	}
	if record.Format != "png" {
		t.Fatalf("report_bug screenshot record format: got %q, want png", record.Format)
	}
	if _, err := base64.StdEncoding.DecodeString(record.Base64); err != nil {
		t.Fatalf("report_bug screenshot record base64: got invalid encoding: %v", err)
	}
}

func TestReportBugSkipsScreenshotPathWhenCaptureFails(t *testing.T) {
	drv := mockdriver.New()
	drv.ScreenshotErr = errors.New("capture failed")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	_, started := callTool(t, cs, ctx, "start_run", map[string]any{"name": "screenshot capture failure"})
	if started["ok"] != true {
		t.Fatalf("start_run: got %v", started)
	}

	result, bug := callTool(t, cs, ctx, "report_bug", map[string]any{
		"title":       "Crash on submit",
		"description": "App crashes when tapping submit.",
	})
	if result.IsError {
		t.Fatalf("report_bug (screenshot capture fails): unexpected isError: %v", bug)
	}
	if bug["ok"] != true {
		t.Fatalf("report_bug (screenshot capture fails): got %v, want ok:true", bug)
	}
	if _, present := bug["screenshotPath"]; present {
		t.Fatalf("report_bug (screenshot capture fails): expected no screenshotPath key, got %v", bug)
	}
}
