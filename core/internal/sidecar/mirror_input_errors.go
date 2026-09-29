package sidecar

// DriverNotReadyError reports that a mirror input action was
// rejected because its target device has no active session
// selection, or its platform agent has not yet reached the ready
// provisioning state. Mirror actions that arrive while an agent is
// still starting up are rejected rather than queued, so they never
// replay as ghost inputs once the agent comes up.
type DriverNotReadyError struct {
	Message string
}

func (e *DriverNotReadyError) Error() string { return e.Message }

// StreamingTouchNotSupportedError reports that the currently
// selected device's driver does not implement
// driver.StreamingTouchCapable, so the caller must fall back to the
// classified touch path.
type StreamingTouchNotSupportedError struct {
	Message string
}

func (e *StreamingTouchNotSupportedError) Error() string { return e.Message }
