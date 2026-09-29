package netcapture

import (
	"context"
	"strings"
	"testing"
)

func TestNullCaptureWaitForRequestReturnsGuidanceError(t *testing.T) {
	nc := NullCapture{}
	_, err := nc.WaitForRequest(context.Background(), "POST /api/test", 1000)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "capture adapter") {
		t.Fatalf("error %q does not mention a capture adapter", err.Error())
	}
	if !strings.Contains(err.Error(), `capture("POST /api/test")`) {
		t.Fatalf("error %q does not name the pattern", err.Error())
	}
}

func TestNullCaptureAllReturnsEmpty(t *testing.T) {
	nc := NullCapture{}
	if all := nc.All(); len(all) != 0 {
		t.Fatalf("All: got %v, want empty", all)
	}
}

func TestNullCaptureStartStopResetAreNoops(t *testing.T) {
	nc := NullCapture{}
	if err := nc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := nc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	nc.Reset()
}
