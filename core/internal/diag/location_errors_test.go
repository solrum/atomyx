package diag_test

import (
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestLocationErrors_CodesMessagesAndRemedies(t *testing.T) {
	cause := errors.New("console closed")
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantInMsg  string
		wantRemedy string
	}{
		{"unsupported", diag.NewLocationUnsupported(""), diag.CodeLocationUnsupported, "does not allow its location", "emulator or a simulator"},
		{"unsupported with reason", diag.NewLocationUnsupported("a phone needs a mock location provider"), diag.CodeLocationUnsupported, "mock location provider", "emulator or a simulator"},
		{"not set", diag.NewLocationNotSet("", nil), diag.CodeLocationNotSet, "was not changed", "booted and reachable"},
		{"not set with reason", diag.NewLocationNotSet("the console said KO", cause), diag.CodeLocationNotSet, "console said KO", "booted and reachable"},
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
