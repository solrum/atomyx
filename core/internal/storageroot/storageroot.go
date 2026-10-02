// Package storageroot resolves the directory every Atomyx binary
// keeps its persistent records in, so the CLI and the MCP server agree
// on one root.
package storageroot

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// EnvVar names the environment variable that overrides the default
	// root.
	EnvVar = "ATOMYX_STORAGE_DIR"

	// HomeDirName is the directory under the user's home that serves as
	// the root when nothing overrides it.
	HomeDirName = ".atomyx"
)

// Resolve returns the storage root. An explicit non-empty path wins,
// then the EnvVar environment variable, then HomeDirName under the
// user's home directory. It fails only when the home directory is
// needed and cannot be determined.
func Resolve(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if fromEnv := os.Getenv(EnvVar); fromEnv != "" {
		return fromEnv, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, HomeDirName), nil
}
