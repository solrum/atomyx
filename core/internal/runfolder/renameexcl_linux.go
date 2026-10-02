package runfolder

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/unix"
)

// renameNoReplace moves oldPath to newPath and fails with an error
// matching fs.ErrExist when newPath exists, even as an empty directory.
func renameNoReplace(oldPath, newPath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		return fs.ErrExist
	}
	return err
}
