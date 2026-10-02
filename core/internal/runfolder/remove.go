package runfolder

import (
	"errors"
	"io/fs"
	"os"
)

// RemoveRun deletes the run folder with the given id under root and
// reports whether one existed. A folder that vanishes while it is being
// removed counts as already removed.
func RemoveRun(root, id string) (bool, error) {
	if !ValidRunID(id) {
		return false, nil
	}
	dir := RunDir(root, id)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, nil
}
