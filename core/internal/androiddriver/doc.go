// Package androiddriver implements driver.Driver over the Android
// agent's HTTP control server. Transport-backed methods delegate to
// an injected Transport; app-lifecycle methods that have no HTTP
// route (install, uninstall, clear data, install check, and the
// adb-forward tunnel connect and disconnect rely on) run through an
// injected process.Runner that shells out to adb, so both seams are
// substitutable in a test without a real device.
package androiddriver
