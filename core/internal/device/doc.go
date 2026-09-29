// Package device discovers the Android and iOS devices reachable
// from the current host and builds a live driver.Driver plus
// orchestra.Orchestra pair for whichever one a caller selects.
//
// Discovery (Probe) and construction (Factory) are separate
// concerns: a Probe answers "what is reachable right now" from adb
// and xcrun output, while a Factory answers "give me a working
// driver for this specific device" by dispatching on platform and
// wiring the matching concrete adapter. Neither depends on the
// sidecar's transport or session state.
package device
