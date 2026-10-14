package setup

import (
	"context"
	"fmt"
)

// AgentProtocolProbe reads the protocol version of the agent on an
// Android device without connecting a driver.
type AgentProtocolProbe interface {
	// AgentProtocol returns the protocol version the agent reports,
	// zero when it reports none, and the one this host requires. It
	// returns an error only when the agent cannot be reached.
	AgentProtocol(ctx context.Context, serial string) (agent, host int, err error)
}

// AgentReinstaller replaces the agent on an Android device with the
// one built from the local source tree.
type AgentReinstaller interface {
	// ReinstallAgent installs the resolved APK over the current agent
	// and brings it up again.
	ReinstallAgent(ctx context.Context, serial string) error
}

// AndroidAgentProtocolStepID is the stable identifier the step reports.
const AndroidAgentProtocolStepID = "android-agent-protocol"

type androidAgentProtocolStep struct {
	serial      string
	probe       AgentProtocolProbe
	reinstaller AgentReinstaller
	services    AccessibilityReader
}

// NewAndroidAgentProtocolStep returns the step that requires the
// agent to speak the protocol version this host supports. Its fix
// reinstalls the agent once, checks again, and states which
// accessibility services the reinstall disabled.
func NewAndroidAgentProtocolStep(serial string, probe AgentProtocolProbe, reinstaller AgentReinstaller, services AccessibilityReader) Fixer {
	return androidAgentProtocolStep{serial: serial, probe: probe, reinstaller: reinstaller, services: services}
}

func (androidAgentProtocolStep) ID() string { return AndroidAgentProtocolStepID }

const protocolRemedy = "Build the agent from the same source tree as this host with cd platforms/android-agent && ./gradlew :app:assembleDebug, " +
	"or set ATOMYX_ANDROID_APK to a current agent APK, then run atomyx setup again."

func (s androidAgentProtocolStep) Check(ctx context.Context) Result {
	agent, host, err := s.probe.AgentProtocol(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the agent protocol version", err,
			"Run atomyx setup again once the agent is ready.")
	}
	if agent == host {
		return Result{Status: StatusOK, Message: fmt.Sprintf("the agent speaks protocol version %d", agent)}
	}
	return Result{Status: StatusManual, Message: mismatchMessage(agent, host), Remedy: protocolRemedy}
}

func (s androidAgentProtocolStep) Fix(ctx context.Context) Result {
	_, before, err := s.services.AccessibilityServices(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the enabled accessibility services", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if err := s.reinstaller.ReinstallAgent(ctx, s.serial); err != nil {
		return failedFrom("could not reinstall the agent", err, protocolRemedy)
	}
	agent, host, err := s.probe.AgentProtocol(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the agent protocol version after the reinstall", err, protocolRemedy)
	}
	if agent != host {
		return Result{
			Status:  StatusFailed,
			Message: "after a reinstall " + mismatchMessage(agent, host),
			Remedy:  protocolRemedy,
		}
	}
	message := fmt.Sprintf("reinstalled the agent, which now speaks protocol version %d", agent)
	return Result{Status: StatusFixed, Message: withDisabledServices(message, before)}
}

func mismatchMessage(agent, host int) string {
	return fmt.Sprintf("the agent speaks protocol version %d but this host requires version %d", agent, host)
}
