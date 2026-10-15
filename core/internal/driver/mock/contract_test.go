package mock_test

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/driver/mock"
)

// simulatedUnreachablePing builds the diag-coded error the ping-
// failure hook injects, matching the code a real adapter attaches
// when its own transport goes unreachable.
func simulatedUnreachablePing() error {
	return diag.New(
		diag.CodeAgentUnreachable,
		"drivertest: simulated unreachable agent",
		"Confirm the agent is running on the device, then try again.",
		nil,
	)
}

func TestDriverSatisfiesContract(t *testing.T) {
	var d *mock.Driver
	drivertest.Run(t, func() driver.Driver {
		d = mock.New()
		return d
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		DeviceCalls: func() int { return len(d.Calls) },
		FailPing:    func() { d.FailNextPingForContract(simulatedUnreachablePing()) },
	}))
}

func TestStreamingDriverSatisfiesContract(t *testing.T) {
	var d *mock.StreamingDriver
	drivertest.Run(t, func() driver.Driver {
		d = mock.NewStreamingTouchCapable()
		return d
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		DeviceCalls: func() int { return len(d.Calls) },
		FailPing:    func() { d.FailNextPingForContract(simulatedUnreachablePing()) },
	}))
}

func TestDriverWithForeignAutomationSatisfiesContract(t *testing.T) {
	var d *mock.Driver
	drivertest.Run(t, func() driver.Driver {
		d = mock.New()
		d.SetForeignAutomation(driver.ForeignAutomationSession{Name: "WebDriverAgentRunner-Runner", Kind: "WebDriverAgent"})
		return d
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		DeviceCalls: func() int { return len(d.Calls) },
		FailPing:    func() { d.FailNextPingForContract(simulatedUnreachablePing()) },
	}))
}

func TestPreInstalledFixtureIsRestoredAfterRoundtrip(t *testing.T) {
	const fixtureID = "dev.atomyx.drivertest.preinstalled"
	var d *mock.Driver
	t.Run("suite", func(t *testing.T) {
		drivertest.Run(t, func() driver.Driver {
			d = mock.New()
			d.InstalledApps[fixtureID] = struct{}{}
			return d
		}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
			DeviceCalls: func() int { return len(d.Calls) },
			FailPing:    func() { d.FailNextPingForContract(simulatedUnreachablePing()) },
			Install:     &drivertest.InstallFixture{Source: fixtureID, AppID: fixtureID},
		}))
	})
	// The roundtrip is the last case, so d is the driver it used.
	if _, ok := d.InstalledApps[fixtureID]; !ok {
		t.Fatal("pre-installed fixture was not installed again after the roundtrip case")
	}
}
