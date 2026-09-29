package diag_test

import (
	"errors"
	"fmt"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestDescribe_PlainErrorReportsUnknown(t *testing.T) {
	err := errors.New("boom")

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeUnknown {
		t.Fatalf("code = %q, want %q", code, diag.CodeUnknown)
	}
	if message != "boom" {
		t.Fatalf("message = %q, want %q", message, "boom")
	}
	if remedy != "" {
		t.Fatalf("remedy = %q, want empty", remedy)
	}
}

func TestDescribe_CodedErrorReportsCodeMessageAndRemedy(t *testing.T) {
	err := diag.New(diag.CodeDeviceNotFound, "no active device", "select a device first", nil)

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeDeviceNotFound {
		t.Fatalf("code = %q, want %q", code, diag.CodeDeviceNotFound)
	}
	if message != "no active device" {
		t.Fatalf("message = %q, want %q", message, "no active device")
	}
	if remedy != "select a device first" {
		t.Fatalf("remedy = %q, want %q", remedy, "select a device first")
	}
}

func TestDescribe_WalksMultipleWrappingLayersToFindTheCodedCause(t *testing.T) {
	cause := diag.New(diag.CodeAgentUnreachable, "could not reach the driver", "reconnect the device", nil)
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", cause))

	code, message, remedy := diag.Describe(wrapped)
	if code != diag.CodeAgentUnreachable {
		t.Fatalf("code = %q, want %q", code, diag.CodeAgentUnreachable)
	}
	if message != "could not reach the driver" {
		t.Fatalf("message = %q, want the coded cause's own message, got %q", message, message)
	}
	if remedy != "reconnect the device" {
		t.Fatalf("remedy = %q, want %q", remedy, "reconnect the device")
	}
}

func TestNew_UnwrapReturnsCause(t *testing.T) {
	cause := errors.New("root cause")
	err := diag.New(diag.CodeUnknown, "wrapped", "", cause)

	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(err, cause) = false, want true")
	}
}
