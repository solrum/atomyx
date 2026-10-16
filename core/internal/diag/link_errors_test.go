package diag_test

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestLinkErrors_CodesMessagesAndRemedies(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantInMsg  string
		wantRemedy string
	}{
		{"unsupported", diag.NewLinkUnsupported(""), diag.CodeLinkUnsupported, "cannot open links", "Launch the app directly"},
		{"unsupported with reason", diag.NewLinkUnsupported("needs a newer system"), diag.CodeLinkUnsupported, "needs a newer system", "Launch the app directly"},
		{"not opened", diag.NewLinkNotOpened(""), diag.CodeLinkNotOpened, "was not opened", "handles this link"},
		{"not opened with reason", diag.NewLinkNotOpened("no handler"), diag.CodeLinkNotOpened, "no handler", "handles this link"},
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
