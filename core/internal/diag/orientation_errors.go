package diag

// NewOrientationUnsupported builds the CodeOrientationUnsupported error
// for a device that cannot be rotated. reason says why in terms the
// caller can act on, and may be empty.
func NewOrientationUnsupported(reason string) error {
	message := "this device cannot be rotated"
	if reason != "" {
		message += ": " + reason
	}
	return New(
		CodeOrientationUnsupported,
		message,
		"Rotate the device by hand, or run on a device that allows rotation to be set.",
		nil,
	)
}

// NewOrientationNotSet builds the CodeOrientationNotSet error for a
// rotation request that reached the device but did not change the
// screen. reason carries the explanation and may be empty. cause is the
// underlying error and may be nil.
func NewOrientationNotSet(reason string, cause error) error {
	message := "the orientation was not changed"
	if reason != "" {
		message += ": " + reason
	}
	return New(
		CodeOrientationNotSet,
		message,
		"Check that the app in front supports the orientation, then try again.",
		cause,
	)
}
