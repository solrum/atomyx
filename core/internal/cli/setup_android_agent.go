package cli

import (
	"context"
	"errors"
	"os/exec"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/setup"
)

var errSetupHostMissing = errors.New("this build has no Android device tooling")

// absentAndroidHost stands in when the composition root configured no
// Android tooling, which only a test does. Every call reports that.
type absentAndroidHost struct{}

func (absentAndroidHost) ResolveAgentAPK() (string, bool) { return "", false }
func (absentAndroidHost) AgentInstalled(context.Context, string) (bool, error) {
	return false, errSetupHostMissing
}
func (absentAndroidHost) InstallAgent(context.Context, string) error { return errSetupHostMissing }
func (absentAndroidHost) ReinstallAgent(context.Context, string) error {
	return errSetupHostMissing
}
func (absentAndroidHost) AccessibilityServices(context.Context, string) (bool, []string, error) {
	return false, nil, errSetupHostMissing
}
func (absentAndroidHost) EnableAgentAccessibility(context.Context, string) error {
	return errSetupHostMissing
}
func (absentAndroidHost) AgentHealth(context.Context, string) (bool, bool, error) {
	return false, false, errSetupHostMissing
}
func (absentAndroidHost) AgentProtocol(context.Context, string) (int, int, error) {
	return 0, 0, errSetupHostMissing
}

func (d Deps) androidAgent() setup.AndroidAgentHost {
	if d.AndroidAgent == nil {
		return absentAndroidHost{}
	}
	return d.AndroidAgent
}

// pathLocator finds tools on the host search path.
type pathLocator struct{}

func (pathLocator) Locate(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}

func (d Deps) toolLocator() setup.ToolLocator {
	if d.Tools == nil {
		return pathLocator{}
	}
	return d.Tools
}

// probeDeviceStates reads a device's adb state from the device scan the
// CLI already uses for device resolution.
type probeDeviceStates struct{ probe device.Probe }

func (p probeDeviceStates) DeviceState(ctx context.Context, serial string) string {
	if p.probe == nil {
		return setup.DeviceAbsent
	}
	for _, desc := range p.probe.Scan(ctx) {
		if desc.Platform != device.PlatformAndroid || desc.ID != serial {
			continue
		}
		switch desc.State {
		case device.StateOnline:
			return setup.DeviceOnline
		case device.StateOffline:
			return setup.DeviceOffline
		case device.StateUnavailable:
			return setup.DeviceUnavailable
		default:
			return setup.DeviceUnauthorized
		}
	}
	return setup.DeviceAbsent
}

func (d Deps) androidDeviceStates() setup.DeviceStateReader {
	return probeDeviceStates{probe: d.Probe}
}

func androidAdbEntry(t setupTarget) setup.Entry {
	return setup.Entry{Step: setup.NewAndroidAdbStep(t.Deps.toolLocator())}
}

func androidDeviceEntry(t setupTarget) setup.Entry {
	return setup.Entry{
		Step:     setup.NewAndroidDeviceStep(t.DeviceID, t.Deps.androidDeviceStates()),
		Requires: []string{setup.AndroidAdbStepID},
	}
}

func androidAPKEntry(t setupTarget) setup.Entry {
	return setup.Entry{
		Step:     setup.NewAndroidAPKStep(t.Deps.androidAgent()),
		Requires: []string{setup.AndroidAdbStepID},
	}
}

func androidAgentInstalledEntry(t setupTarget) setup.Entry {
	return setup.Entry{
		Step:     setup.NewAndroidAgentInstalledStep(t.DeviceID, t.Deps.androidAgent()),
		Requires: []string{setup.AndroidDeviceStepID},
	}
}

func androidAccessibilityEntry(t setupTarget) setup.Entry {
	return setup.Entry{
		Step:     setup.NewAndroidAccessibilityStep(t.DeviceID, t.Deps.androidAgent()),
		Requires: []string{setup.AndroidAgentInstalledStepID},
	}
}

func androidAgentHealthEntry(t setupTarget) setup.Entry {
	return setup.Entry{
		Step:     setup.NewAndroidAgentHealthStep(t.DeviceID, t.Deps.androidAgent()),
		Requires: []string{setup.AndroidAccessibilityStepID},
	}
}

func androidAgentProtocolEntry(t setupTarget) setup.Entry {
	host := t.Deps.androidAgent()
	return setup.Entry{
		Step:     setup.NewAndroidAgentProtocolStep(t.DeviceID, host, host),
		Requires: []string{setup.AndroidAgentHealthStepID},
	}
}
