package setup

import "context"

// AgentInstaller reads and changes whether the Atomyx agent is
// installed on an Android device.
type AgentInstaller interface {
	// AgentInstalled reports whether the agent package is installed on
	// the device with the given serial.
	AgentInstalled(ctx context.Context, serial string) (bool, error)
	// InstallAgent installs the agent from the resolved APK and brings
	// it up.
	InstallAgent(ctx context.Context, serial string) error
}

// AndroidAgentInstalledStepID is the stable identifier the step reports.
const AndroidAgentInstalledStepID = "android-agent-installed"

type androidAgentInstalledStep struct {
	serial    string
	installer AgentInstaller
	apk       APKResolver
	services  AccessibilityReader
}

// NewAndroidAgentInstalledStep returns the step that requires the
// agent to be installed on the device. Its fix installs the agent and
// states which accessibility services that disabled, because the
// install rebinds accessibility. When no agent APK exists the fix
// reports the android-apk step instead of attempting the install.
func NewAndroidAgentInstalledStep(serial string, installer AgentInstaller, apk APKResolver, services AccessibilityReader) Fixer {
	return androidAgentInstalledStep{serial: serial, installer: installer, apk: apk, services: services}
}

func (androidAgentInstalledStep) ID() string { return AndroidAgentInstalledStepID }

func (s androidAgentInstalledStep) Check(ctx context.Context) Result {
	installed, err := s.installer.AgentInstalled(ctx, s.serial)
	if err != nil {
		return failedFrom("could not tell whether the agent is installed", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if installed {
		return Result{Status: StatusOK, Message: "the agent is installed on " + s.serial}
	}
	return Result{
		Status:  StatusManual,
		Message: "the agent is not installed on " + s.serial,
		Remedy:  "Run atomyx setup without --check to install it.",
	}
}

func (s androidAgentInstalledStep) Fix(ctx context.Context) Result {
	if _, found := s.apk.ResolveAgentAPK(); !found {
		return Result{
			Status:  StatusManual,
			Message: "the agent APK was not found, so the agent cannot be installed",
			Remedy:  "Resolve the " + AndroidAPKStepID + " step first, then run atomyx setup again.",
		}
	}
	_, before, err := s.services.AccessibilityServices(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the enabled accessibility services", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if err := s.installer.InstallAgent(ctx, s.serial); err != nil {
		return failedFrom("could not install the agent", err,
			"Check the adb output, make sure the device is unlocked and allows installs over USB, then run atomyx setup again.")
	}
	return Result{Status: StatusFixed, Message: withDisabledServices("installed the agent on "+s.serial, before)}
}
