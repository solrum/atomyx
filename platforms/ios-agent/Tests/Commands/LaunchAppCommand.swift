import Foundation

/// Launch an app by bundle id and update driver state so subsequent
/// inspect / action commands know which `XCUIApplication` to use.
///
/// XCUITest cannot query arbitrary foreground apps — every inspect or
/// tap must run against an `XCUIApplication` reference the driver has
/// already launched. `launchApp` is therefore not just a convenience:
/// it's the state-setting operation that makes the rest of the driver
/// work. See `.claude/docs/ios.md` for the full rationale.
final class LaunchAppCommand: CommandHandler {
    let type = "launchApp"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let bundleId = request.args["bundleId"] as? String, !bundleId.isEmpty else {
            return .error(id: request.id, message: "missing bundleId")
        }
        let app = bridge.launchApp(bundleId: bundleId)
        state.currentApp = app
        state.currentBundleId = bundleId
        return .ok(id: request.id, data: [
            "bundleId": bundleId,
            "state": app.state.rawValue,
        ])
    }
}
