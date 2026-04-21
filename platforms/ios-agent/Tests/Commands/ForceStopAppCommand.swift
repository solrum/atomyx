import Foundation

/// Terminate an app by bundle id. Safe to call on apps that aren't
/// currently running — `XCUIApplication.terminate()` is idempotent.
///
/// If the terminated app matches the driver's tracked currentApp, the
/// tracking is cleared so a subsequent `dumpTree` / `tapAt` fails fast
/// with a clear error instead of silently querying a dead process.
final class ForceStopAppCommand: CommandHandler {
    let type = "forceStopApp"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let bundleId = request.args["bundleId"] as? String, !bundleId.isEmpty else {
            return .error(id: request.id, message: "missing bundleId")
        }
        bridge.terminateApp(bundleId: bundleId)
        if state.currentBundleId == bundleId {
            state.currentApp = nil
            state.currentBundleId = ""
        }
        return .ok(id: request.id, data: ["bundleId": bundleId])
    }
}
