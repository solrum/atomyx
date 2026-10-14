package cli

import "dev.atomyx/internal/setup"

// setupTarget is what a step constructor knows about the device being
// prepared. DeviceID is empty when no device was named and none could
// be resolved, in which case steps that need a device report that.
type setupTarget struct {
	Platform string
	DeviceID string
	Deps     Deps
}

// setupStepFactory builds one registered step for a target and names
// the steps that must pass before it runs.
type setupStepFactory func(target setupTarget) setup.Entry

// setupStepsByPlatform is the single place that lists the setup steps
// of each platform, in run order. Adding a step is one new file that
// declares its factory and one line here.
var setupStepsByPlatform = map[string][]setupStepFactory{
	"android": {
		androidAdbEntry,
		androidDeviceEntry,
		androidAPKEntry,
		androidAgentInstalledEntry,
		androidAccessibilityEntry,
		androidAgentHealthEntry,
		androidAgentProtocolEntry,
	},
	"ios": {},
}
