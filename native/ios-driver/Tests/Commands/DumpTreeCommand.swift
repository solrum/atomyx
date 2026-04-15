import Foundation

/// Dump the accessibility tree of the currently-tracked app. Filtering
/// (stable-signal only, no wrappers) happens in the bridge — this
/// command just serializes the result onto the wire.
///
/// **Always** uses the tracked `state.currentApp` reference set by
/// `LaunchAppCommand`. There is NO `bundleId` override because:
///
///   1. `XCUIApplication(bundleIdentifier:)` without `launch()` gives a
///      reference to an unlaunched process — XCUITest queries against
///      it return 0 elements silently, not a clear error. Easy to
///      misinterpret as "screen is empty".
///   2. The tracked reference is the same object returned by
///      `launch()`, which XCUITest binds to the running process state.
///      Creating a second reference would bypass that binding.
///
/// Stale state handling: if `state.currentApp` is nil, return a
/// structured error. The host adapter treats this as a signal to clear
/// its own `lastLaunchedBundleId` cache and surface the error to the
/// agent with a clear "call launchApp first" message.
///
/// Request args:
///   - `limit` (optional, default 200): cap on post-filter element
///     count to keep wire size bounded.
///
/// Response data:
///   - `bundleId`: the currently-tracked bundle id (for correlation)
///   - `total`: pre-filter descendant count
///   - `count`: post-filter element count in this response
///   - `truncated`: true when `count == limit && total > limit`
///   - `elements`: array of element dicts matching the wire schema
///     documented in docs/ios.md
final class DumpTreeCommand: CommandHandler {
    let type = "dumpTree"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(
                id: request.id,
                message: "no app launched — call launchApp first"
            )
        }

        let limit = (request.args["limit"] as? Int) ?? 200
        let dump = bridge.dumpElements(app: app, limit: limit)

        let elementDicts: [[String: Any]] = dump.elements.map { el in
            var item: [String: Any] = [
                "type": el.type,
                "id": el.identifier,
                "label": el.label,
                "enabled": el.enabled,
                "hittable": el.hittable,
                "x": Int(el.frame.midX),
                "y": Int(el.frame.midY),
                "w": Int(el.frame.width),
                "h": Int(el.frame.height),
            ]
            if let value = el.value {
                item["value"] = value
            }
            return item
        }

        return .ok(id: request.id, data: [
            "bundleId": state.currentBundleId,
            "total": dump.total,
            "count": elementDicts.count,
            "truncated": elementDicts.count >= limit && dump.total > limit,
            "elements": elementDicts,
        ])
    }
}
