package device

import (
	"context"
	"errors"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/version"
)

// AndroidAgentSetupDeps configures an AndroidAgentSetup.
type AndroidAgentSetupDeps struct {
	Runner process.Runner
	Clock  orchestra.Clock
	// APKPath is the agent APK to install. Empty falls back to the
	// ATOMYX_ANDROID_APK environment variable, then to the built
	// output in the source tree.
	APKPath string
}

// AndroidAgentSetup reads and repairs the Atomyx agent on an Android
// device for the setup command. It reaches the agent through adb and
// the agent's HTTP routes, never through a driver, so a broken agent
// cannot fail a connect. Every method takes the serial of the device
// it acts on.
type AndroidAgentSetup struct {
	runner  process.Runner
	clock   orchestra.Clock
	apkPath string
}

// NewAndroidAgentSetup returns an AndroidAgentSetup. A nil Clock
// defaults to orchestra.SystemClock{}.
func NewAndroidAgentSetup(deps AndroidAgentSetupDeps) *AndroidAgentSetup {
	clk := deps.Clock
	if clk == nil {
		clk = orchestra.SystemClock{}
	}
	return &AndroidAgentSetup{runner: deps.Runner, clock: clk, apkPath: deps.APKPath}
}

// ResolveAgentAPK reports the agent APK path and whether a file exists
// there, resolved the way the agent launcher resolves it.
func (s *AndroidAgentSetup) ResolveAgentAPK() (string, bool) {
	return androiddriver.ResolveAgentAPK(s.apkPath)
}

// AgentInstalled reports whether the agent package is installed.
func (s *AndroidAgentSetup) AgentInstalled(ctx context.Context, serial string) (bool, error) {
	return androiddriver.NewAgentInspector(s.runner, serial).Installed(ctx)
}

// InstallAgent installs the agent when it is not already answering and
// brings it up.
func (s *AndroidAgentSetup) InstallAgent(ctx context.Context, serial string) error {
	launcher, _, err := newAndroidAgentLauncher(ctx, s.runner, s.clock, serial, s.apkPath)
	if err != nil {
		return err
	}
	return launcher.EnsureRunning(ctx)
}

// ReinstallAgent replaces the agent with the resolved APK even when it
// is answering.
func (s *AndroidAgentSetup) ReinstallAgent(ctx context.Context, serial string) error {
	launcher, _, err := newAndroidAgentLauncher(ctx, s.runner, s.clock, serial, s.apkPath)
	if err != nil {
		return err
	}
	return launcher.Reinstall(ctx)
}

// AccessibilityServices reports whether the Atomyx accessibility
// service is enabled and which other services are.
func (s *AndroidAgentSetup) AccessibilityServices(ctx context.Context, serial string) (agentEnabled bool, others []string, err error) {
	return androiddriver.NewAgentInspector(s.runner, serial).AccessibilityServices(ctx)
}

// EnableAgentAccessibility makes the Atomyx accessibility service the
// only enabled one.
func (s *AndroidAgentSetup) EnableAgentAccessibility(ctx context.Context, serial string) error {
	launcher, _, err := newAndroidAgentLauncher(ctx, s.runner, s.clock, serial, s.apkPath)
	if err != nil {
		return err
	}
	return launcher.EnableAccessibility(ctx)
}

// AgentHealth asks the agent's health route whether it is ready and
// has its accessibility service bound. An agent that answers with an
// error status is reported as not ready. An error is returned only when
// the agent cannot be reached.
func (s *AndroidAgentSetup) AgentHealth(ctx context.Context, serial string) (ready, accessibilityConnected bool, err error) {
	_, client, err := newAndroidAgentLauncher(ctx, s.runner, s.clock, serial, s.apkPath)
	if err != nil {
		return false, false, err
	}
	health, err := client.Health(ctx)
	var agentErr *transport.AgentError
	if errors.As(err, &agentErr) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return health.Ok, health.AccessibilityConnected, nil
}

// AgentProtocol reads the protocol version the agent reports on its
// ping route, together with the version this host requires. An agent
// that answers with an error status predates the route and counts as
// version 0. An error is returned only when the agent cannot be
// reached.
func (s *AndroidAgentSetup) AgentProtocol(ctx context.Context, serial string) (agent, host int, err error) {
	_, client, err := newAndroidAgentLauncher(ctx, s.runner, s.clock, serial, s.apkPath)
	if err != nil {
		return 0, 0, err
	}
	pong, pingErr := client.Ping(ctx)
	agentVersion, known := androiddriver.PingedProtocolVersion(pong, pingErr)
	if !known {
		return 0, 0, pingErr
	}
	return agentVersion, version.AgentProtocolVersion, nil
}
