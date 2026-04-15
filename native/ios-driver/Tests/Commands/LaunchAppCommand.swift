import Foundation

/// Launch an app by bundle id and update driver state so subsequent
/// inspect / action commands know which `XCUIApplication` to use.
///
/// Week 1 finding #1: XCUITest cannot query arbitrary foreground apps.
/// Every inspect or tap must run against an `XCUIApplication` reference
/// the driver has already launched. `launchApp` is therefore not just a
/// convenience — it's the state-setting operation that makes the rest
/// of the driver work.
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
