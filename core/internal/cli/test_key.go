package cli

import (
	"os"
	"path/filepath"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/runfolder"
)

const gitEntryName = ".git"

func holdsGitRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, gitEntryName))
	return err == nil
}

// testKeyOf returns the test key of the script or scenario file at path,
// which may be relative to the working directory. A path that cannot be
// made absolute is keyed as given. When the file exists, symlinks in the
// path are resolved first, so a link to a script keys the script itself
// and the key does not depend on which alias the caller used.
func testKeyOf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return runfolder.DeriveTestKey(config.HasConfigFile, holdsGitRoot, abs)
}
