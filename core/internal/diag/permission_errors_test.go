package diag_test

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestPermissionErrors_CodesMessagesAndRemedies(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantInMsg  string
		wantRemedy string
	}{
		{"unsupported", diag.NewPermissionsUnsupported(""), diag.CodePermissionsUnsupported, "does not allow permissions", "device settings"},
		{"unsupported with reason", diag.NewPermissionsUnsupported("camera needs a newer system"), diag.CodePermissionsUnsupported, "needs a newer system", "device settings"},
		{"not set", diag.NewPermissionNotSet("", nil), diag.CodePermissionNotSet, "was not changed", "declares the permission"},
		{"not set with reason", diag.NewPermissionNotSet("app not installed", nil), diag.CodePermissionNotSet, "app not installed", "declares the permission"},
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
