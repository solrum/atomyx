package diag

// NewLinkUnsupported builds the CodeLinkUnsupported error for a device
// that cannot open links. reason says why in terms the caller can act on,
// and may be empty.
func NewLinkUnsupported(reason string) error {
	message := "this device cannot open links"
	if reason != "" {
		message += ": " + reason
	}
	return New(
		CodeLinkUnsupported,
		message,
		"Launch the app directly instead, or run on a device that can open links.",
		nil,
	)
}

// NewLinkNotOpened builds the CodeLinkNotOpened error for a link that
// reached the device but that no app opened. reason carries the device's
// own explanation and may be empty.
func NewLinkNotOpened(reason string) error {
	message := "the link was not opened"
	if reason != "" {
		message += ": " + reason
	}
	return New(
		CodeLinkNotOpened,
		message,
		"Check that an installed app handles this link, then try again.",
		nil,
	)
}
