package process

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrProcessNotFound reports that no process has the inspected pid.
var ErrProcessNotFound = errors.New("process not found")

// ProcessInfo is the identity of one live process, read from the
// operating system in a single query so its fields describe the same
// process.
type ProcessInfo struct {
	PID int
	// PGID is the process group the process belongs to.
	PGID int
	// UID is the effective user id of the process.
	UID int
	// Command is the executable name without its path. The operating
	// system truncates it to its own limit, 15 characters on Linux and
	// 16 on macOS.
	Command string
	// StartTime is when the process started. Paired with a pid it
	// identifies one process for the lifetime of the host, because the
	// operating system can hand a pid to an unrelated process after the
	// first one exits.
	StartTime time.Time
}

// Inspector reads process and host identity facts. A reader that
// decides whether a recorded pid is still the recorded process takes
// an Inspector by constructor injection, so a test substitutes a fake.
type Inspector interface {
	// Inspect returns the identity of pid, or an error wrapping
	// ErrProcessNotFound when no such process exists.
	Inspect(pid int) (ProcessInfo, error)
	// BootTime reports when the host booted. A pid recorded before a
	// reboot can name an unrelated process afterwards, so a different
	// boot time invalidates every recorded pid.
	BootTime() (time.Time, error)
}

// SystemInspector is the Inspector backed by the operating system.
type SystemInspector struct{}

var _ Inspector = SystemInspector{}

// Inspect implements Inspector.
func (SystemInspector) Inspect(pid int) (ProcessInfo, error) {
	if pid <= 0 {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
	}
	return inspectProcess(pid)
}

// BootTime implements Inspector.
func (SystemInspector) BootTime() (time.Time, error) { return hostBootTime() }

// commandBase reduces an executable path to its file name.
func commandBase(path string) string { return filepath.Base(path) }

// OwnProcessGroup reports the process group of the running process.
func OwnProcessGroup() int { return ownProcessGroup() }

// OwnUID reports the effective user id of the running process.
func OwnUID() int { return os.Geteuid() }
