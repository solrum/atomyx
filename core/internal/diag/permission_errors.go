package diag

// NewPermissionsUnsupported builds the CodePermissionsUnsupported error
// for a device on which permissions cannot be set. reason says why in
// terms the caller can act on, and may be empty.
func NewPermissionsUnsupported(reason string) error {
	message := "this device does not allow permissions to be set"
	if reason != "" {
		message = reason
	}
	return New(
		CodePermissionsUnsupported,
		message,
		"Grant the permission in the device settings or answer the prompt in the app, or run on a device that allows permissions to be set.",
		nil,
	)
}

// NewPermissionNotSet builds the CodePermissionNotSet error for a request
// that reached the device but did not change a permission. reason carries
// the device's own explanation and may be empty.
func NewPermissionNotSet(reason string, cause error) error {
	message := "the permission was not changed"
	if reason != "" {
		message += ": " + reason
	}
	return New(
		CodePermissionNotSet,
		message,
		"Check that the app is installed and declares the permission, then try again.",
		cause,
	)
}
