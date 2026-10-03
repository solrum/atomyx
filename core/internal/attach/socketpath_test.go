package attach

import (
	"os"
	"path/filepath"
	"testing"
)

// shortSocketPath returns a socket path in a fresh directory. The path
// stays under the 104 byte limit of macOS unix sockets.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ax")
	if err != nil || len(dir) > 60 {
		dir, err = os.MkdirTemp("/tmp", "ax")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "a.sock")
}
