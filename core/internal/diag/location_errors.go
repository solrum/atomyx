package diag

// NewLocationUnsupported builds the CodeLocationUnsupported error for a
// device whose location the driver cannot set. reason says why in terms
// the caller can act on, and may be empty.
func NewLocationUnsupported(reason string) error {
	message := "this device does not allow its location to be set"
	if reason != "" {
		message = reason
	}
	return New(
		CodeLocationUnsupported,
		message,
		"Run on an emulator or a simulator, or set the location in the device settings.",
		nil,
	)
}

// NewLocationNotSet builds the CodeLocationNotSet error for a request
// that reached the device but did not move it. reason carries the
// explanation and may be empty. cause is the underlying error and may be
// nil.
func NewLocationNotSet(reason string, cause error) error {
	message := "the location was not changed"
	if reason != "" {
		message += ": " + reason
	}
	return New(
		CodeLocationNotSet,
		message,
		"Check that the device is booted and reachable, then try again.",
		cause,
	)
}
