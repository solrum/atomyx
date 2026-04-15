import type { DeviceController, ForegroundInfo } from "../adapters/device-controller.port.js";

export type PreflightResult =
  | { ok: true }
  | {
      ok: false;
      reason: string;
      diagnostic: {
        treeElementCount: number;
        foreground: ForegroundInfo | null;
      };
      rebindHint: string;
    };

/**
 * Platform-specific rebind instructions. Each entry is a platform → shell-
 * command-block mapping. The Android block is the only one filled in today;
 * iOS will append its own "reset WDA" / "restart simulator" commands when
 * the iOS adapter lands.
 */
const REBIND_HINTS: Record<string, string> = {
  android:
    "Run these three adb commands from the host shell:\n" +
    "  adb shell \"settings delete secure enabled_accessibility_services\"\n" +
    "  adb shell \"settings put secure enabled_accessibility_services dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService\"\n" +
    "  adb shell \"am start-foreground-service -n dev.atomyx.agent/.control.AtomyxForegroundService\"",
  ios:
    "iOS rebind path is platform-specific and depends on the chosen bridge. " +
    "See docs/ios.md for the current approach (WDA restart / simctl bootstrap / ...).",
};

/**
 * Verify the device is actually producing accessibility data. The HTTP
 * /health endpoint reports "connected" as long as the service is bound, but
 * the service can lose its `rootInActiveWindow` handle silently after an
 * APK install, doze, or foreground-service restart — the result is an empty
 * tree on every call. This preflight catches that pattern early so agents
 * get a clear actionable error instead of cascading failures.
 *
 * Heuristic: tree must return ≥1 element AND `currentForeground()` must
 * report a non-empty appId. If both are empty, the platform binding is
 * stale and needs the platform-specific rebind procedure.
 */
export async function preflight(ctl: DeviceController): Promise<PreflightResult> {
  // iOS semantics differ fundamentally from Android. On Android, a
  // healthy AccessibilityService always exposes SOMETHING (home
  // screen, launcher, current foreground app) — empty tree +
  // unknown foreground genuinely signals stale binding.
  //
  // On iOS, XCUITest can only query apps it has launched. Right
  // after `select_device`, the driver's `state.currentApp` is nil
  // and `getUiSummary` / `currentForeground` legitimately return
  // empty. Applying the Android heuristic here would flag every
  // fresh iOS session as "stale". This is the one place we
  // intentionally platform-branch at the tool-layer helper level —
  // the alternative (per-adapter preflight method) adds port
  // surface for a single cross-cutting health check. Pragmatic
  // exception documented in docs/pitfalls.md.
  //
  // If the iOS driver is unreachable, `IosXctestController.connect()`
  // has already failed upstream during the select_device call — the
  // tool dispatcher never reaches this point.
  if (ctl.platform === "ios") {
    return { ok: true };
  }

  const summary = await ctl.getUiSummary().catch(() => []);
  const foreground = await ctl
    .currentForeground()
    .catch(() => ({ appId: "", screen: undefined }) as ForegroundInfo);

  const treeEmpty = summary.length === 0;
  const foregroundEmpty = !foreground.appId;

  if (treeEmpty && foregroundEmpty) {
    return {
      ok: false,
      reason:
        "Device binding is stale: tree is empty AND foreground app is unknown. " +
        "The accessibility / automation backend is reachable but has lost its active-window handle " +
        "(typical after app install, doze, or backend restart). Rebind the service and retry.",
      diagnostic: {
        treeElementCount: summary.length,
        foreground,
      },
      rebindHint: REBIND_HINTS[ctl.platform] ?? REBIND_HINTS.android,
    };
  }

  return { ok: true };
}
