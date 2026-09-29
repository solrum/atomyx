// Package drivertest holds the driver contract suite: the
// invariants any driver.Driver implementation must satisfy,
// expressed as table-driven tests that accept a factory constructing
// fresh instances. mock.Driver, androiddriver.AndroidDriver,
// iosdriver.IosDriver, and iossimdriver.IosSimDriver are all checked
// against Run, each with its own fake-backed factory.
//
// A second suite, gated by RequireDevice and the "e2e" build tag,
// runs the same Run against a real device or simulator instead of a
// fake. It never runs in a normal build or CI job: build with
// "-tags e2e" and set ATOMYX_DRIVER_E2E to the platform label
// (android, ios, iossim, or all) to opt in, for example:
//
//	ATOMYX_DRIVER_E2E=android go test -tags e2e ./internal/androiddriver/...
package drivertest
