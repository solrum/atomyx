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
	// DefaultDebugPauseTimeoutMs is how long a run stays paused for a
	// person to debug before it resumes on its own.
	DefaultDebugPauseTimeoutMs int64 = 1_800_000
	// DefaultDeviceAcquireTimeoutMs bounds one device acquisition,
	// including the launch of an iOS runner, but not a runner build.
	DefaultDeviceAcquireTimeoutMs int64 = 120_000
	// DefaultRunnerBuildTimeoutMs bounds one runner build, including the
	// wait for another process's build of the same runner. It is a fixed
	// bound, not a configuration key.
	DefaultRunnerBuildTimeoutMs int64 = 300_000
)

// Timeouts are the time budgets a long-lived session host applies.
type Timeouts struct {
	// McpIdleMs is the idle release timeout with no run open.
	McpIdleMs int64
	// OpenRunIdleMs is the idle release timeout with a run open.
	OpenRunIdleMs int64
	// DebugPauseMs bounds how long a paused run waits for a person before
	// it resumes on its own.
	DebugPauseMs int64
	// DeviceAcquireMs bounds one device acquisition.
	DeviceAcquireMs int64
	// RunnerBuildMs bounds the runner build that Prepare runs before the
	// lease is taken.
	RunnerBuildMs int64
}

// DefaultTimeouts returns the default budgets.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		McpIdleMs:       DefaultMcpIdleTimeoutMs,
		OpenRunIdleMs:   DefaultOpenRunIdleTimeoutMs,
		DebugPauseMs:    DefaultDebugPauseTimeoutMs,
		DeviceAcquireMs: DefaultDeviceAcquireTimeoutMs,
		RunnerBuildMs:   DefaultRunnerBuildTimeoutMs,
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
	if t.DebugPauseMs <= 0 {
		t.DebugPauseMs = d.DebugPauseMs
	}
	if t.DeviceAcquireMs <= 0 {
		t.DeviceAcquireMs = d.DeviceAcquireMs
	}
	if t.RunnerBuildMs <= 0 {
		t.RunnerBuildMs = d.RunnerBuildMs
	}
	return t
}
