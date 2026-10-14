package setup

import (
	"context"
	"fmt"
)

// AgentProtocol pairs the wire protocol version an agent reports with
// the one this host supports. Agent is zero for an agent that reports
// none.
type AgentProtocol struct {
	Agent int
	Host  int
}

// Matches reports whether the agent speaks the version the host
// requires.
func (p AgentProtocol) Matches() bool { return p.Agent == p.Host }

// AgentProtocolProbe reads the protocol version of the agent on an
// Android device without connecting a driver.
type AgentProtocolProbe interface {
	// AgentProtocol returns the agent's and the host's protocol
	// versions, or an error when the agent cannot be reached.
	AgentProtocol(ctx context.Context, serial string) (AgentProtocol, error)
}

// AgentReinstaller replaces the agent on an Android device with the
// one built from the local source tree.
type AgentReinstaller interface {
	// ReinstallAgent installs the resolved APK over the current agent
	// and brings it up again.
	ReinstallAgent(ctx context.Context, serial string) error
}

const androidAgentProtocolStepID = "android-agent-protocol"

type androidAgentProtocolStep struct {
	serial      string
	probe       AgentProtocolProbe
	reinstaller AgentReinstaller
}

// NewAndroidAgentProtocolStep returns the step that requires the
// agent to speak the protocol version this host supports. Its fix
// reinstalls the agent once and checks again.
func NewAndroidAgentProtocolStep(serial string, probe AgentProtocolProbe, reinstaller AgentReinstaller) Fixer {
	return androidAgentProtocolStep{serial: serial, probe: probe, reinstaller: reinstaller}
}

func (androidAgentProtocolStep) ID() string { return androidAgentProtocolStepID }

const protocolRemedy = "Build the agent from the same source tree as this host with cd platforms/android-agent && ./gradlew :app:assembleDebug, " +
	"or set ATOMYX_ANDROID_APK to a current agent APK, then run atomyx setup again."

func (s androidAgentProtocolStep) Check(ctx context.Context) Result {
	protocol, err := s.probe.AgentProtocol(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the agent protocol version", err,
			"Run atomyx setup again once the agent is ready.")
	}
	if protocol.Matches() {
		return Result{Status: StatusOK, Message: fmt.Sprintf("the agent speaks protocol version %d", protocol.Agent)}
	}
	return Result{Status: StatusManual, Message: mismatchMessage(protocol), Remedy: protocolRemedy}
}

func (s androidAgentProtocolStep) Fix(ctx context.Context) Result {
	if err := s.reinstaller.ReinstallAgent(ctx, s.serial); err != nil {
		return failedFrom("could not reinstall the agent", err, protocolRemedy)
	}
	protocol, err := s.probe.AgentProtocol(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the agent protocol version after the reinstall", err, protocolRemedy)
	}
	if !protocol.Matches() {
		return Result{
			Status:  StatusFailed,
			Message: "after a reinstall " + mismatchMessage(protocol),
			Remedy:  protocolRemedy,
		}
	}
	return Result{Status: StatusFixed, Message: fmt.Sprintf("reinstalled the agent, which now speaks protocol version %d", protocol.Agent)}
}

func mismatchMessage(p AgentProtocol) string {
	return fmt.Sprintf("the agent speaks protocol version %d but this host requires version %d", p.Agent, p.Host)
}
