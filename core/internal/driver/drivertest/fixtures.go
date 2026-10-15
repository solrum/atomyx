package drivertest

import (
	"testing"

	"dev.atomyx/internal/driver"
)

// syntheticAppID is the app id used for every fixture a synthetic
// driver accepts, since it treats any id as installable.
const syntheticAppID = "dev.atomyx.drivertest.synthetic"

// WithSyntheticFixtures returns h with every unset fixture filled for
// a driver that runs against a mock or a fake transport: any app id
// is launchable, clearable, and installable there, no input needs
// focusing, and nothing is in front before the first launch. A caller
// driving a real device must not use it, because those assumptions
// fail on hardware. Fixtures already set in h are kept.
func WithSyntheticFixtures(h Hooks) Hooks {
	if h.FocusInput == nil {
		h.FocusInput = func(*testing.T, driver.Driver) {}
	}
	if h.NoForeground == nil {
		h.NoForeground = func(*testing.T, driver.Driver) {}
	}
	if h.LaunchableAppID == "" {
		h.LaunchableAppID = syntheticAppID
	}
	if h.ClearableAppID == "" {
		h.ClearableAppID = syntheticAppID
	}
	if h.Install == nil {
		h.Install = &InstallFixture{Source: syntheticAppID, AppID: syntheticAppID}
	}
	return h
}
