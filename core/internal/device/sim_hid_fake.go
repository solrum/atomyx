package device

import "sync"

// FakeSimHidStatusProvider is a device-free SimHidStatusProvider test
// double: a test stages the status to return per UDID directly, with
// no launcher, process, or real helper involved.
type FakeSimHidStatusProvider struct {
	mu       sync.Mutex
	statuses map[string]SimHidStatus
}

// NewFakeSimHidStatusProvider constructs a FakeSimHidStatusProvider
// that reports idle for every UDID until SetStatus stages otherwise.
func NewFakeSimHidStatusProvider() *FakeSimHidStatusProvider {
	return &FakeSimHidStatusProvider{statuses: make(map[string]SimHidStatus)}
}

// SetStatus makes the next Status calls for udid return status.
func (f *FakeSimHidStatusProvider) SetStatus(udid string, status SimHidStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[udid] = status
}

// Status implements sidecar.SimHidStatusProvider.
func (f *FakeSimHidStatusProvider) Status(udid string) SimHidStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	if status, ok := f.statuses[udid]; ok {
		return status
	}
	return SimHidStatus{UDID: udid, State: SimHidStateIdle}
}
