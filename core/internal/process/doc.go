// Package process abstracts external command execution behind the
// Runner interface, so an adapter that shells out to a device-tooling
// CLI (adb, xcrun, and similar) can be driven from a test without
// spawning a real process or requiring a connected device.
package process
