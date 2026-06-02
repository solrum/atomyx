import Foundation
import XCTest

/// Dump the accessibility tree as a nested dictionary. Preserves
/// parent → child structure so host-side consumers that need
/// structural context (ancestor-descendant walks, preceding-
/// sibling lookups, layout-aware matchers) can operate on it.
///
/// Request args:
///   - none: dump the tracked `currentApp` (set by `launchApp`).
///   - `bundleIdCandidates: [String]`: when `currentApp` is not
///     bound, the host can hand the runner a shortlist of bundle
///     ids that are currently running on the simulator. The
///     runner queries each one's `XCUIApplication.state` and
///     picks the first whose state is `.runningForeground`. This
///     unblocks an inspector that wants to dump whatever the user
///     is looking at, without forcing an explicit `launchApp`
///     call.
///
/// `XCUIApplication()` with no bundle id is intentionally not
/// used here — the runner is a standalone XCTest bundle with no
/// `TEST_HOST`, so the no-arg resolver traps before the snapshot
/// call lands.
///
/// Response data:
///   - `root`: nested dict with fields
///     `{elementType, identifier, label, value?, enabled, bounds, children?}`
///     — maps one-to-one onto the host-side raw-element wire type.
///   - `bundleId` (optional): set when the runner had to attach
///     to a candidate to fulfil the request, so the host can
///     remember it for follow-up calls.
final class DumpRawTreeCommand: CommandHandler {
    let type = "dumpRawTree"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        var attachedBundleId: String? = nil
        let app: XCUIApplication

        if let bound = state.currentApp {
            app = bound
        } else if let candidates = request.args["bundleIdCandidates"] as? [String], !candidates.isEmpty {
            guard let resolved = Self.firstForegroundCandidate(candidates) else {
                return .error(
                    id: request.id,
                    message: "no candidate bundle id is currently running in the foreground (\(candidates.count) tried)"
                )
            }
            // Cache for subsequent calls — the host saves a roundtrip
            // and we keep snapshot/touch routing consistent.
            state.currentApp = resolved.app
            state.currentBundleId = resolved.bundleId
            attachedBundleId = resolved.bundleId
            app = resolved.app
        } else {
            return .error(
                id: request.id,
                message: "no app bound — pass bundleIdCandidates or call launchApp first"
            )
        }

        guard let root = bridge.dumpRawTree(app: app) else {
            return .error(id: request.id, message: "snapshot failed — driver or simulator may be unresponsive")
        }

        var data: [String: Any] = ["root": root]
        if let bid = attachedBundleId { data["bundleId"] = bid }
        return .ok(id: request.id, data: data)
    }

    /// Probe each candidate via `XCUIApplication.state` and return
    /// the first one currently in `.runningForeground`. Background
    /// or not-running apps are skipped. Probing is cheap — it does
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
