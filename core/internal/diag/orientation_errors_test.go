package diag_test

import (
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestOrientationErrors_CodesMessagesAndRemedies(t *testing.T) {
	cause := errors.New("size read failed")
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantInMsg  string
		wantRemedy string
	}{
		{"unsupported", diag.NewOrientationUnsupported(""), diag.CodeOrientationUnsupported, "cannot be rotated", "Rotate the device by hand"},
		{"unsupported with reason", diag.NewOrientationUnsupported("locked by policy"), diag.CodeOrientationUnsupported, "locked by policy", "Rotate the device by hand"},
		{"not set", diag.NewOrientationNotSet("", nil), diag.CodeOrientationNotSet, "was not changed", "supports the orientation"},
		{"not set with reason", diag.NewOrientationNotSet("the screen stayed upright", cause), diag.CodeOrientationNotSet, "stayed upright", "supports the orientation"},
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
