package androiddriver

import (
	"os/exec"
	"testing"
)

// TestShellQuote_SurvivesAPosixShell hands each quoted value to a real
// shell and expects the shell to print the original text back, which is
// the property the device shell needs from the quoting.
func TestShellQuote_SurvivesAPosixShell(t *testing.T) {
	values := []string{
		"plain",
		"a b",
		"a&b",
		"it's",
		`"double"`,
		"$HOME `id` $(id)",
		"semi;colon|pipe",
		`back\slash`,
		"'",
		"''",
		"new\nline",
	}
	for _, value := range values {
		out, err := exec.CommandContext(t.Context(), "sh", "-c", "printf %s "+shellQuote(value)).Output()
		if err != nil {
			t.Fatalf("sh: %v", err)
		}
		if string(out) != value {
			t.Errorf("shell printed %q for %q, want the original", out, value)
		}
	}
}
