package iosdriver

// WaitForeignAutomationProbe blocks until the probe started by the last
// Connect has finished, so a test can read the cache deterministically.
func (d *IosDriver) WaitForeignAutomationProbe() { d.foreignWG.Wait() }
