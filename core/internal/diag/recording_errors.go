package diag

// NewRecordingUnsupported builds the CodeRecordingUnsupported error for a
// device that cannot record its screen.
func NewRecordingUnsupported() error {
	return New(
		CodeRecordingUnsupported,
		"this device cannot record its screen",
		"Continue without a video, or run on a device that supports screen recording.",
		nil,
	)
}
