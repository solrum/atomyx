package session

// Default idle and acquisition budgets, in milliseconds.
const (
	// DefaultMcpIdleTimeoutMs is how long an MCP session with no tool call
	// and no open run keeps its device before releasing it.
	DefaultMcpIdleTimeoutMs int64 = 600_000
	// DefaultOpenRunIdleTimeoutMs is how long an MCP session with an open
	// run and no tool call keeps its device before releasing it and
	// closing the run.
	DefaultOpenRunIdleTimeoutMs int64 = 600_000
	// DefaultDeviceAcquireTimeoutMs bounds one device acquisition,
	// including the launch of an iOS runner, but not a runner build.
	DefaultDeviceAcquireTimeoutMs int64 = 120_000
)

// Timeouts are the time budgets a long-lived session host applies.
type Timeouts struct {
	// McpIdleMs is the idle release timeout with no run open.
	McpIdleMs int64
	// OpenRunIdleMs is the idle release timeout with a run open.
	OpenRunIdleMs int64
	// DeviceAcquireMs bounds one device acquisition.
	DeviceAcquireMs int64
}

// DefaultTimeouts returns the default budgets.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		McpIdleMs:       DefaultMcpIdleTimeoutMs,
		OpenRunIdleMs:   DefaultOpenRunIdleTimeoutMs,
		DeviceAcquireMs: DefaultDeviceAcquireTimeoutMs,
	}
}

// WithDefaults returns t with every non-positive field replaced by its
// default.
func (t Timeouts) WithDefaults() Timeouts {
	d := DefaultTimeouts()
	if t.McpIdleMs <= 0 {
		t.McpIdleMs = d.McpIdleMs
	}
	if t.OpenRunIdleMs <= 0 {
		t.OpenRunIdleMs = d.OpenRunIdleMs
	}
	if t.DeviceAcquireMs <= 0 {
		t.DeviceAcquireMs = d.DeviceAcquireMs
	}
	return t
}
