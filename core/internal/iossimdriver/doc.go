// Package iossimdriver implements driver.Driver over the iOS
// Simulator's direct-HID input path. Touch gestures (tap, long-press,
// swipe, and streaming down/move/up) are dispatched through a
// WebSocket connection to the atomyx-sim-hid helper process, which
// wraps Apple's private SimDeviceLegacyHIDClient framework. Every
// other Driver method delegates to an embedded iosdriver.IosDriver
// (XCUITest-backed), because the HID layer has no equivalent for
// hierarchy capture, screenshot, app lifecycle, or text input.
//
// IsSimDirectSupported gates whether the current host can run this
// path at all (arm64 plus a verified Xcode floor) by shelling out
// through an injected process.Runner; starting the atomyx-sim-hid
// helper itself and deciding when to opt into this path are the
// caller's responsibility.
package iossimdriver
