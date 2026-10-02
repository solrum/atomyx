package diag_test

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestLeaseErrors_CodesAndRemedies(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantInMsg  string
		wantRemedy string
	}{
		{"held", diag.NewDeviceHeld("pid 7 (atomyx run), running"), diag.CodeDeviceHeld, "pid 7", "Stop that process"},
		{"released held", diag.NewDeviceReleasedHeld("atomyx run, running"), diag.CodeDeviceReleasedHeld, "atomyx run", "Retry the call shortly"},
		{"holder busy", diag.NewHolderBusy("a tool call is running"), diag.CodeHolderBusy, "a tool call is running", "ask again"},
		{"run expired", diag.NewRunExpired("r-1"), diag.CodeRunExpired, "r-1", "Start a new run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg, remedy := diag.Describe(tt.err)
			if code != tt.wantCode {
				t.Fatalf("code = %q, want %q", code, tt.wantCode)
			}
			if !strings.Contains(msg, tt.wantInMsg) {
				t.Fatalf("message %q lacks %q", msg, tt.wantInMsg)
			}
			if !strings.Contains(remedy, tt.wantRemedy) {
				t.Fatalf("remedy %q lacks %q", remedy, tt.wantRemedy)
			}
		})
	}
}

func TestDeviceHeldIsDistinctFromDeviceBusy(t *testing.T) {
	if diag.CodeDeviceHeld == diag.CodeDeviceBusy {
		t.Fatal("DEVICE_HELD must not equal DEVICE_BUSY")
	}
}
