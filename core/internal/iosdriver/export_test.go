package iosdriver

import "time"

// WaitForeignAutomationProbe blocks until the probe started by the last
// Connect has finished, so a test can read the cache deterministically.
func (d *IosDriver) WaitForeignAutomationProbe() { d.foreignWG.Wait() }

// SetTunnelPingTimeout shortens the probe timeout StartTunnel uses and
// returns a function that restores it.
func SetTunnelPingTimeout(d time.Duration) (restore func()) {
	old := tunnelPingTimeout
	tunnelPingTimeout = d
	return func() { tunnelPingTimeout = old }
}
