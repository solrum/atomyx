package device

import "context"

// IOSAgent is a running iOS runner a provisioner hands to a caller.
type IOSAgent struct {
	// Port is the host port the runner answers on.
	Port int
	// RunnerID is the launch identifier the runner echoes in its ping
	// answer. It is empty when the provisioner does not report one.
	RunnerID string
	// Adopted reports that the runner was already running, left by an
	// earlier holder of the device, instead of launched by this call.
	Adopted bool
	// Kill stops the runner, along with its tunnel and record. It is never
	// nil and runs its work once however many times it is called.
	Kill func() error
}

// ProvisionOptions adjusts one ProvisionIOSAgent call.
type ProvisionOptions struct {
	// Fresh makes the call end any recorded runner and launch a new one,
	// even when the recorded runner could be adopted. A caller sets it
	// after an adopted runner proved unusable.
	Fresh bool
}

// AdoptingProvisioner is an AgentProvisioner that reuses a runner an
// earlier holder left running.
type AdoptingProvisioner interface {
	AgentProvisioner
	// ProvisionIOSAgent behaves like EnsureIOSAgentWatched and also
	// reports the launch identifier and whether the runner was adopted.
	// The error contract of EnsureIOSAgentWatched applies, including that
	// Kill is never nil.
	ProvisionIOSAgent(ctx context.Context, udid string, kind IosAgentKind, onLost func(error), opts ProvisionOptions) (IOSAgent, error)
}
