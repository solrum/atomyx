import Foundation
import XCTest

/// Press a system or keyboard key.
///
/// Returns an affordance-reporting response so the host adapter can map
/// to a truthful `ActionResult`. See `DeviceActor.pressKey` docs in the
/// TS port for the semantics of each key.
///
/// Strategy chain for "back" (in order):
///   1. Nav bar back button (verifiable) → affordanceFound=true
///   2. Edge swipe fallback (unverifiable) → affordanceFound=false
///
/// `home` is app-independent; short-circuits before the `currentApp`
/// check so agents can call it from the home screen / cold state.
/// `back` and `enter` require a tracked app (coordinate space /
/// keyboard context).
///
/// Response data:
///   - `key`: echoed back for correlation
///   - `affordanceFound`: true iff a verifiable control was used
///   - `strategy`: which path was taken (nav_bar_back | edge_swipe_best_effort
///                 | home | enter | typed_raw)
final class PressKeyCommand: CommandHandler {
    let type = "pressKey"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let key = request.args["key"] as? String, !key.isEmpty else {
            return .error(id: request.id, message: "missing key")
        }

        // `home` is device-wide — short-circuit before the currentApp
        // check. Pass a dummy XCUIApplication reference; DefaultXCUIBridge
        // ignores it for the home key path.
        if key == "home" {
            let result = bridge.pressKey(app: XCUIApplication(), key: "home")
            return .ok(id: request.id, data: [
                "key": "home",
                "affordanceFound": result.affordanceFound,
                "strategy": result.strategy,
            ])
        }

        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }
        let result = bridge.pressKey(app: app, key: key)
        return .ok(id: request.id, data: [
            "key": key,
            "affordanceFound": result.affordanceFound,
            "strategy": result.strategy,
        ])
    }
}
