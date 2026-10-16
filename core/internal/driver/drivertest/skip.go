package drivertest

import "testing"

// SkipKey is the stable, kebab-case identifier of one reason a test may
// skip. Every skip in the shared corpus and in the device e2e tests goes
// through SkipBecause with one of these constants, so a typo is a
// compile error and a skipped run can be audited by key. The device
// e2e script compares the keys found in a run against the committed
// allowlist in scripts/e2e-skip-allowlist.txt, one section per leg.
type SkipKey string

const (
	// SkipLegNotSelected marks a device-gated test that does not belong to
	// the leg being run, because ATOMYX_DRIVER_E2E is unset or names a
	// different platform. The script counts these but never gates on them.
	SkipLegNotSelected SkipKey = "leg-not-selected"

	// SkipHostToolMissing marks a host tool the selected leg needs (adb,
	// xcrun) that is not on PATH.
	SkipHostToolMissing SkipKey = "host-tool-missing"

	// SkipDeviceNotFound marks a selected leg with no connected device,
	// emulator or booted simulator.
	SkipDeviceNotFound SkipKey = "device-not-found"

	// SkipDeviceDiscoveryFailed marks a selected leg whose device listing
	// command failed.
	SkipDeviceDiscoveryFailed SkipKey = "device-discovery-failed"

	// SkipRequiredEnvUnset marks a variable the selected leg must provide
	// (the target serial or UDID).
	SkipRequiredEnvUnset SkipKey = "required-env-unset"

	// SkipOptionalEnvUnset marks a case that needs optional variables the
	// operator did not provide, such as a second device.
	SkipOptionalEnvUnset SkipKey = "optional-env-unset"

	// SkipHostPrecondition marks a host property the test depends on, such
	// as processor architecture, a non-root user, or a symlink-capable
	// filesystem.
	SkipHostPrecondition SkipKey = "host-precondition"

	// SkipNoLaunchableApp marks a driver wired without a LaunchableAppID.
	SkipNoLaunchableApp SkipKey = "no-launchable-app"

	// SkipNoClearableApp marks a driver wired without a ClearableAppID.
	SkipNoClearableApp SkipKey = "no-clearable-app"

	// SkipNoInstallFixture marks a driver wired without an Install fixture.
	SkipNoInstallFixture SkipKey = "no-install-fixture"

	// SkipNoFocusInput marks a driver wired without a FocusInput hook.
	SkipNoFocusInput SkipKey = "no-focus-input"

	// SkipNoLaunchArgumentsFixture marks a driver wired without a
	// LaunchArguments fixture.
	SkipNoLaunchArgumentsFixture SkipKey = "no-launch-arguments-fixture"

	// SkipNoOpenLinkFixture marks a driver wired without an OpenLink
	// fixture.
	SkipNoOpenLinkFixture SkipKey = "no-open-link-fixture"

	// SkipNoOpenLink marks a driver that reports CanOpenLink false.
	SkipNoOpenLink SkipKey = "no-open-link"

	// SkipNoPermissionsFixture marks a driver that can set permissions but
	// is wired without a Permissions fixture.
	SkipNoPermissionsFixture SkipKey = "no-permissions-fixture"

	// SkipNoFailPing marks a driver wired without a FailPing hook.
	SkipNoFailPing SkipKey = "no-fail-ping"

	// SkipNoFailConnect marks a driver wired without a FailConnect hook.
	SkipNoFailConnect SkipKey = "no-fail-connect"

	// SkipNoHierarchyStaging marks a driver that cannot stage a known
	// hierarchy, which is true of every real device.
	SkipNoHierarchyStaging SkipKey = "no-hierarchy-staging"

	// SkipNoDefaultHierarchy marks a driver that has no hierarchy to
	// report before one is staged.
	SkipNoDefaultHierarchy SkipKey = "no-default-hierarchy"

	// SkipNoKeyCodes marks a driver that advertises no supported key codes.
	SkipNoKeyCodes SkipKey = "no-key-codes"

	// SkipNoStreamingTouch marks a driver without the optional streaming
	// touch capability.
	SkipNoStreamingTouch SkipKey = "no-streaming-touch"

	// SkipNoForeignAutomation marks a driver without the optional foreign
	// automation reporter.
	SkipNoForeignAutomation SkipKey = "no-foreign-automation"
)

// SkipBecause skips the running test or benchmark and tags the reason
// with key, in the form "skip[key]: detail". The tag is what the e2e
// script greps for, so a skip must never be written any other way.
func SkipBecause(tb testing.TB, key SkipKey, detail string) {
	tb.Helper()
	tb.Skipf("skip[%s]: %s", key, detail)
}
