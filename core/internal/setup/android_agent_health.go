package setup

import "context"

// AgentHealthProbe asks the agent on an Android device for its
// health without connecting a driver.
type AgentHealthProbe interface {
	// AgentHealth reports whether the agent answers its health route
	// as ready and whether its accessibility service is bound, which
	// every agent action needs. It returns an error only when the
	// agent cannot be reached.
	AgentHealth(ctx context.Context, serial string) (ready, accessibilityConnected bool, err error)
}

// AgentStarter starts an installed agent on an Android device.
type AgentStarter interface {
	// StartAgent starts the agent's control server and waits until it
	// answers. It neither reinstalls the agent nor changes which
	// accessibility services are enabled.
	StartAgent(ctx context.Context, serial string) error
}

// AndroidAgentHealthStepID is the stable identifier the step reports.
const AndroidAgentHealthStepID = "android-agent-health"

const agentHealthStartRemedy = "Open the Atomyx app on the device, then run atomyx setup again."

type androidAgentHealthStep struct {
	serial  string
	probe   AgentHealthProbe
	starter AgentStarter
}

// NewAndroidAgentHealthStep returns the step that requires the agent
// to answer as ready with its accessibility service connected. Its fix
// starts an agent that does not answer, for example after a reboot or
// a force stop, and checks again. An agent that answers but is not
// ready needs the operator, so the fix leaves that result unchanged.
func NewAndroidAgentHealthStep(serial string, probe AgentHealthProbe, starter AgentStarter) Fixer {
	return androidAgentHealthStep{serial: serial, probe: probe, starter: starter}
}

func (androidAgentHealthStep) ID() string { return AndroidAgentHealthStepID }

func (s androidAgentHealthStep) Check(ctx context.Context) Result {
	ready, connected, err := s.probe.AgentHealth(ctx, s.serial)
	if err != nil {
		return Result{
			Status:  StatusManual,
			Message: "the agent did not answer",
			Remedy:  "Run atomyx setup without --check to start it, or open the Atomyx app on the device.",
		}
	}
	switch {
	case !ready:
		return Result{
			Status:  StatusManual,
			Message: "the agent answered but reports that it is not ready",
			Remedy:  "Unlock the device, wait a few seconds, then run atomyx setup again.",
		}
	case !connected:
		return Result{
			Status:  StatusManual,
			Message: "the agent is running but its accessibility service is not connected",
			Remedy:  "Turn the Atomyx accessibility service off and on in Settings, Accessibility on the device, then run atomyx setup again.",
		}
	}
	return Result{Status: StatusOK, Message: "the agent is ready"}
}

func (s androidAgentHealthStep) Fix(ctx context.Context) Result {
	if _, _, err := s.probe.AgentHealth(ctx, s.serial); err == nil {
		return s.Check(ctx)
	}
	if err := s.starter.StartAgent(ctx, s.serial); err != nil {
		return failedFrom("could not start the agent", err, agentHealthStartRemedy)
	}
	after := s.Check(ctx)
	if after.Status == StatusOK {
		return Result{Status: StatusFixed, Message: "started the agent, which is now ready"}
	}
	return after
}
