package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"dev.atomyx/internal/logging"
)

func decodeFields(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("output is not a single JSON object: %v, got %q", err, buf.String())
	}
	fields, _ := decoded["fields"].(map[string]any)
	return fields
}

func TestChildFromContext_TagsEntriesWithTheDeviceAndTheRun(t *testing.T) {
	var buf bytes.Buffer
	base := logging.NewStructuredLogger(&buf, logging.LevelDebug)
	ctx := logging.WithRunID(context.Background(), "run-9")
	ctx = logging.WithDevice(ctx, logging.DeviceTag{DeviceID: "emulator-5554", HardwareID: "Pixel_6@emulator-5554"})

	logging.ChildFromContext(base, ctx).Info("step", nil)

	fields := decodeFields(t, &buf)
	if fields["runId"] != "run-9" || fields["deviceId"] != "emulator-5554" || fields["hardwareId"] != "Pixel_6@emulator-5554" {
		t.Fatalf("fields = %v, want runId, deviceId and hardwareId", fields)
	}
}

func TestChildFromContext_DeviceWithoutARunStillTagsTheDevice(t *testing.T) {
	var buf bytes.Buffer
	base := logging.NewStructuredLogger(&buf, logging.LevelDebug)
	ctx := logging.WithDevice(context.Background(), logging.DeviceTag{DeviceID: "SIM-1"})

	logging.ChildFromContext(base, ctx).Info("step", nil)

	fields := decodeFields(t, &buf)
	if fields["deviceId"] != "SIM-1" {
		t.Fatalf("fields = %v, want deviceId SIM-1", fields)
	}
	if _, present := fields["hardwareId"]; present {
		t.Fatalf("fields = %v, want no hardwareId when none was given", fields)
	}
	if _, present := fields["runId"]; present {
		t.Fatalf("fields = %v, want no runId when none was given", fields)
	}
}

func TestDeviceFromContext_AnEmptyTagIsAbsent(t *testing.T) {
	if _, ok := logging.DeviceFromContext(context.Background()); ok {
		t.Fatal("DeviceFromContext(background) ok = true")
	}
	ctx := logging.WithDevice(context.Background(), logging.DeviceTag{})
	if _, ok := logging.DeviceFromContext(ctx); ok {
		t.Fatal("DeviceFromContext(empty tag) ok = true, want false because an empty tag identifies nothing")
	}
}
