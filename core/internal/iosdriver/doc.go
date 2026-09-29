// Package iosdriver implements driver.Driver over the Swift XCUITest
// runner's line-delimited JSON TCP protocol. Gesture, text-input,
// keyboard, and app-launch methods delegate to an injected Conn.
// App-lifecycle methods that have no TCP route (install, uninstall,
// clear data, install check, list) run through an injected
// process.Runner that shells out to xcrun simctl or xcrun devicectl,
// branching on whether the driver targets a simulator or a physical
// device. Connecting to a physical device also runs an iproxy USB
// tunnel through the same Runner before the TCP connect, because the
// runner listens on loopback inside the device rather than on the
// host.
package iosdriver
