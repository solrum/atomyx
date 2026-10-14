package driver

// ForeignAutomationSession describes one UI automation session running
// on the target device that Atomyx did not start. Two automation
// sessions driving the same device can interfere with each other, so
// a caller surfaces the finding as a warning.
type ForeignAutomationSession struct {
	// Name is the process or job label that identifies the session.
	Name string `json:"name"`

	// Kind is a short human label for the automation framework, for
	// example "XCUITest runner" or "WebDriverAgent".
	Kind string `json:"kind"`
}

// ForeignAutomationReporter is the capability interface for a driver
// that can tell whether another UI automation session is active on
// its device. A caller checks driver.As[ForeignAutomationReporter]
// and treats a driver that does not implement it as having nothing
// to report.
type ForeignAutomationReporter interface {
	// ForeignAutomation returns the foreign sessions found when the
	// driver connected. It answers from a cache, never contacts the
	// device, and returns nil when none were found or the probe could
	// not run. The returned slice is the caller's to keep.
	ForeignAutomation() []ForeignAutomationSession
}

// AsForeignAutomationReporter reports whether d also implements
// ForeignAutomationReporter. It sees through a decorator.
func AsForeignAutomationReporter(d Driver) (ForeignAutomationReporter, bool) {
	return As[ForeignAutomationReporter](d)
}
