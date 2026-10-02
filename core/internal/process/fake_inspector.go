package process

import (
	"fmt"
	"sync"
	"time"
)

// FakeInspector is a test double for Inspector: it reports the
// processes and boot time a test sets, and records nothing else.
type FakeInspector struct {
	mu    sync.Mutex
	procs map[int]ProcessInfo
	boot  time.Time
	// BootErr, when non-nil, is returned by BootTime.
	BootErr error
}

var _ Inspector = (*FakeInspector)(nil)

// NewFakeInspector returns a FakeInspector reporting boot as the host
// boot time and no live processes.
func NewFakeInspector(boot time.Time) *FakeInspector {
	return &FakeInspector{procs: make(map[int]ProcessInfo), boot: boot}
}

// Set makes Inspect report info for info.PID.
func (f *FakeInspector) Set(info ProcessInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.procs[info.PID] = info
}

// Remove makes Inspect report pid as not found.
func (f *FakeInspector) Remove(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.procs, pid)
}

// Inspect implements Inspector.
func (f *FakeInspector) Inspect(pid int) (ProcessInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.procs[pid]
	if !ok {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
	}
	return info, nil
}

// BootTime implements Inspector.
func (f *FakeInspector) BootTime() (time.Time, error) {
	if f.BootErr != nil {
		return time.Time{}, f.BootErr
	}
	return f.boot, nil
}
