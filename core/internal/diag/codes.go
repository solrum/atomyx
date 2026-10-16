package diag

// Codes lists every stable error code in declaration order. Code
// consumers that must decide something per code, such as the failure
// classification of a run, iterate it so a new code cannot be missed.
func Codes() []string {
	return []string{
		CodeUnknown,
		CodeDeviceNotFound,
		CodeAgentUnreachable,
		CodeAppInstallFailed,
		CodeAppUninstallFailed,
		CodeAppDataClearFailed,
		CodeAppNotInstalled,
		CodeToolTimeout,
		CodeForegroundUnsupported,
		CodeDeviceBusy,
		CodeDeviceHeld,
		CodeDeviceReleasedHeld,
		CodeDevicePinned,
		CodeRunExpired,
		CodeHostPortInUse,
		CodeInvalidAppID,
		CodePointerCaptureUnsupported,
		CodeAgentRejected,
		CodeInvalidArgument,
		CodeAgentCommunicationFailed,
		CodeAgentNotReady,
		CodeAgentVersionMismatch,
		CodeProxyRequired,
		CodeRunnerBuildTimeout,
		CodeRunUnreadable,
		CodeRunReadOnly,
		CodeRunBusy,
		CodeHolderBusy,
		CodeUnsupported,
		CodeUnsupportedVersion,
		CodeRecordingUnsupported,
		CodeLinkUnsupported,
		CodeLinkNotOpened,
		CodePermissionsUnsupported,
		CodePermissionNotSet,
		CodeOrientationUnsupported,
		CodeOrientationNotSet,
		CodeInputNotFocused,
		CodeElementNotFound,
	}
}
