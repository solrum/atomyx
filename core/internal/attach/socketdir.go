package attach

import (
	"fmt"
	"os"
	"syscall"
)

// checkSocketDir refuses a directory that another user could use to reach
// a socket placed in it: one not owned by uid, or open to group or others.
func checkSocketDir(dir string, uid uint32) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("attach: checking socket directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("attach: %s is not a directory", dir)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uid {
		return fmt.Errorf("attach: socket directory %s must be owned by the current user", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("attach: socket directory %s must not be accessible to group or others (mode %04o)", dir, info.Mode().Perm())
	}
	return nil
}
