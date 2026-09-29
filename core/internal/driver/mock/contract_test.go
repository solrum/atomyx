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
	}, drivertest.Hooks{
		FailPing: func() { d.FailNextPingForContract(simulatedUnreachablePing()) },
	})
}

func TestStreamingDriverSatisfiesContract(t *testing.T) {
	var d *mock.StreamingDriver
	drivertest.Run(t, func() driver.Driver {
		d = mock.NewStreamingTouchCapable()
		return d
	}, drivertest.Hooks{
		FailPing: func() { d.FailNextPingForContract(simulatedUnreachablePing()) },
	})
}
