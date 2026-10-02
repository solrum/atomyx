package lease

import "syscall"

// mntLocal is the statfs flag Darwin sets on a filesystem mounted from
// a local device. The syscall package does not export it.
const mntLocal = 0x1000

// isLocalFS reports whether path sits on a filesystem mounted with the
// local flag, so a network mount that cannot arbitrate flock across
// hosts is excluded.
func isLocalFS(path string) (bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, err
	}
	return st.Flags&mntLocal != 0, nil
}
