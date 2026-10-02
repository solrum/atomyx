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

// Resolve returns the storage root as an absolute path. An explicit
// non-empty path wins, then the EnvVar environment variable, then
// HomeDirName under the user's home directory. A relative path is made
// absolute against the working directory at the moment of resolution, so
// every path derived from the root stays valid after the process changes
// directory and can be handed to a caller in another working directory.
// It fails when the home directory is needed and cannot be determined,
// or when the working directory is needed and cannot be read.
func Resolve(explicit string) (string, error) {
	if explicit == "" {
		explicit = os.Getenv(EnvVar)
	}
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("making the storage root absolute: %w", err)
		}
		return abs, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, HomeDirName), nil
}
