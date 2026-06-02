import Foundation

/// Launch an app by bundle id and update driver state so subsequent
/// inspect / action commands know which `XCUIApplication` to use.
///
/// XCUITest cannot query arbitrary foreground apps — every inspect
/// or tap must run against an `XCUIApplication` reference the
/// driver has already launched. `launchApp` is therefore not just a
/// convenience: it is the state-setting operation that makes every
/// subsequent command possible. Skipping it and then calling `tap`
/// or `getUiTree` will fail deterministically.
///
/// `noReset:true` attaches to an already-tracked app instead of
/// relaunching it. Used by the Studio mirror tap path so forwarding
/// a tap to a running app does not wipe its in-memory state. When
/// the driver has no record of the bundle (`currentApp == nil` or
/// `currentBundleId != bundleId`), `noReset` falls back to a fresh
/// launch — the request would otherwise be unfulfillable.
final class LaunchAppCommand: CommandHandler {
    let type = "launchApp"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let bundleId = request.args["bundleId"] as? String, !bundleId.isEmpty else {
            return .error(id: request.id, message: "missing bundleId")
        }
        let noReset = (request.args["noReset"] as? Bool) ?? false
        if noReset, state.currentBundleId == bundleId, let existing = state.currentApp {
            return .ok(id: request.id, data: [
                "bundleId": bundleId,
                "state": existing.state.rawValue,
                "attached": true,
            ])
        }
        let app = bridge.launchApp(bundleId: bundleId)
        state.currentApp = app
        state.currentBundleId = bundleId
        return .ok(id: request.id, data: [
            "bundleId": bundleId,
            "state": app.state.rawValue,
            "attached": false,
        ])
    }
}
