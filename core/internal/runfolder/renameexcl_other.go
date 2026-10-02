//go:build !darwin && !linux

package runfolder

import (
	"io/fs"
	"os"
)

// renameNoReplace moves oldPath to newPath and fails with an error
// matching fs.ErrExist when newPath exists. This platform has no atomic
// no-replace rename, so the check and the rename are two steps, which is
// safe because ImportRun callers hold the migration lock.
func renameNoReplace(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return fs.ErrExist
	}
	return os.Rename(oldPath, newPath)
}
