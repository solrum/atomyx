package cli

import (
	"fmt"
	"os"
	"testing"

	"dev.atomyx/internal/storageroot"
)

// TestMain points the default storage root at a throwaway directory,
// because every run and suite command writes a run folder and a test
// that passes no --report-dir would otherwise write into the real home
// directory.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "atomyx-cli-test-root-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv(storageroot.EnvVar, root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}
