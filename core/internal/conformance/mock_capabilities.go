package conformance

import (
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

// applyCapabilitiesOverride merges o's non-nil fields onto d's
// current capabilities, leaving every nil field at the mock driver's
// existing value. Shared by every corpus that stages a
// RunnerDriverProgram onto a mock.Driver: the runner and sidecar
// differentials both need it.
func applyCapabilitiesOverride(d *mock.Driver, o *RunnerCapabilitiesOverride) {
	caps := d.Capabilities()
	if o.CanScreenshot != nil {
		caps.CanScreenshot = *o.CanScreenshot
	}
	if o.CanEraseText != nil {
		caps.CanEraseText = *o.CanEraseText
	}
	if o.CanWaitForIdle != nil {
		caps.CanWaitForIdle = *o.CanWaitForIdle
	}
	if o.CanSetLocation != nil {
		caps.CanSetLocation = *o.CanSetLocation
	}
	if o.CanSetOrientation != nil {
		caps.CanSetOrientation = *o.CanSetOrientation
	}
	if o.CanHideKeyboard != nil {
		caps.CanHideKeyboard = *o.CanHideKeyboard
	}
	if o.CanOpenLink != nil {
		caps.CanOpenLink = *o.CanOpenLink
	}
	if o.CanMultiPointer != nil {
		caps.CanMultiPointer = *o.CanMultiPointer
	}
	if o.CanPressure != nil {
		caps.CanPressure = *o.CanPressure
	}
	if o.SupportedKeyCodes != nil {
		codes := make([]driver.KeyCode, len(o.SupportedKeyCodes))
		for i, c := range o.SupportedKeyCodes {
			codes[i] = driver.KeyCode(c)
		}
		caps.SupportedKeyCodes = codes
	}
	d.SetCapabilities(caps)
}
