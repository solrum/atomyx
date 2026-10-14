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

const androidAgentInstalledStepID = "android-agent-installed"

type androidAgentInstalledStep struct {
	serial    string
	installer AgentInstaller
}

// NewAndroidAgentInstalledStep returns the step that requires the
// agent to be installed on the device. Its fix installs the agent.
func NewAndroidAgentInstalledStep(serial string, installer AgentInstaller) Fixer {
	return androidAgentInstalledStep{serial: serial, installer: installer}
}

func (androidAgentInstalledStep) ID() string { return androidAgentInstalledStepID }

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
	if err := s.installer.InstallAgent(ctx, s.serial); err != nil {
		return failedFrom("could not install the agent", err,
			"Check the adb output, make sure the device is unlocked and allows installs over USB, then run atomyx setup again.")
	}
	return Result{Status: StatusFixed, Message: "installed the agent on " + s.serial}
}
