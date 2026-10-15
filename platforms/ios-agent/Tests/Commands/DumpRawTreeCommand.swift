import Foundation
import XCTest

/// Dump the accessibility tree as a nested dictionary. Preserves
/// parent-to-child structure so host-side consumers that need
/// structural context (ancestor-descendant walks, preceding-
/// sibling lookups, layout-aware matchers) can operate on it.
///
/// Request args:
///   - none: dump the tracked `currentApp` (set by `launchApp`) while
///     it is running. When the tracked app has exited, the dump falls
///     back to the Springboard proxy.
///   - `bundleIdCandidates: [String]`: when no live app is
///     tracked, the host can hand the runner a shortlist of bundle
///     ids that are currently running on the simulator. The
///     runner queries each one's `XCUIApplication.state` and
///     picks the first whose state is `.runningForeground`. This
///     unblocks an inspector that wants to dump whatever the user
///     is looking at, without forcing an explicit `launchApp`
///     call. When no candidate is in the foreground, the dump is
///     the Springboard tree and nothing is cached as `currentApp`,
///     so a visibility check after the app was terminated
///     externally sees the home screen instead of an error.
///
/// `XCUIApplication()` with no bundle id is intentionally not
/// used here, because the runner is a standalone XCTest bundle with no
/// `TEST_HOST`, so the no-arg resolver traps before the snapshot
/// call lands.
///
/// Response data:
///   - `root`: nested dict with fields
///     `{elementType, identifier, label, value?, enabled, bounds, children?}`
///     maps one-to-one onto the host-side raw-element wire type.
///   - `bundleId` (optional): set when the runner had to attach
///     to a candidate to fulfil the request, so the host can
///     remember it for follow-up calls.
final class DumpRawTreeCommand: CommandHandler {
    let type = "dumpRawTree"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        var attachedBundleId: String? = nil
        let app: XCUIApplication

        let tracked = state.resolveTrackedApp(bridge: bridge)
        if case .running(let bound) = tracked {
            app = bound
        } else if let candidates = request.args["bundleIdCandidates"] as? [String], !candidates.isEmpty {
            if let resolved = Self.firstForegroundCandidate(candidates) {
                // Cache for subsequent calls, so the host saves a roundtrip
                // and we keep snapshot/touch routing consistent.
                state.currentApp = resolved.app
                state.currentBundleId = resolved.bundleId
                attachedBundleId = resolved.bundleId
                app = resolved.app
            } else {
                // Not cached: the home screen is not an app the host asked
                // to drive, and binding it would hide a later launch.
                app = DriverState.springboard()
            }
        } else if case .notRunning = tracked {
            app = DriverState.springboard()
        } else {
            return .error(
                id: request.id,
                message: "no app bound, pass bundleIdCandidates or call launchApp first"
            )
        }

        guard let root = bridge.dumpRawTree(app: app) else {
            return .error(id: request.id, message: "snapshot failed, driver or simulator may be unresponsive")
        }

        var data: [String: Any] = ["root": root]
        if let bid = attachedBundleId { data["bundleId"] = bid }
        return .ok(id: request.id, data: data)
    }

    /// Probe each candidate via `XCUIApplication.state` and return
    /// the first one currently in `.runningForeground`. Background
    /// or not-running apps are skipped. Probing is cheap because it does
    /// not require launching or attaching, only an inspect of the
    /// application proxy state.
    private static func firstForegroundCandidate(_ bundleIds: [String]) -> (app: XCUIApplication, bundleId: String)? {
        for bid in bundleIds {
            let probe = XCUIApplication(bundleIdentifier: bid)
            if probe.state == .runningForeground {
                return (probe, bid)
            }
        }
        return nil
    }
}
