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

// AndroidAgentHealthStepID is the stable identifier the step reports.
const AndroidAgentHealthStepID = "android-agent-health"

type androidAgentHealthStep struct {
	serial string
	probe  AgentHealthProbe
}

// NewAndroidAgentHealthStep returns the step that requires the agent
// to answer as ready with its accessibility service connected. It has
// no fix, because the steps before it already installed and enabled
// everything the agent needs.
func NewAndroidAgentHealthStep(serial string, probe AgentHealthProbe) Step {
	return androidAgentHealthStep{serial: serial, probe: probe}
}

func (androidAgentHealthStep) ID() string { return AndroidAgentHealthStepID }

func (s androidAgentHealthStep) Check(ctx context.Context) Result {
	ready, connected, err := s.probe.AgentHealth(ctx, s.serial)
	if err != nil {
		return failedFrom("the agent did not answer", err,
			"Open the Atomyx agent on the device or run atomyx setup again, and check adb logcat if it keeps failing.")
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
