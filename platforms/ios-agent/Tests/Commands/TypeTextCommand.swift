import Foundation

/// Type text into the currently-focused input field on the tracked app.
///
/// iOS handles typing natively via `XCUIApplication.typeText(_:)` —
/// one call, whatever the system keyboard is showing. XCUITest
/// dispatches per-character events internally, including IME layout
/// switches, so callers do not need to tap individual keys.
///
/// The caller must have tapped (and thus focused) the target input
/// before calling. `typeText` with nothing focused is a silent no-op
/// — XCUITest swallows the event.
///
/// Framework notes (expand this list when adding support):
///
///   - Native UIKit / SwiftUI text inputs: accept `typeText` directly.
///   - Flutter Semantics text fields: accept `typeText` when a system
///     keyboard is presented.
///   - Custom in-app keyboards (e.g. RN or Flutter widgets that
///     render their own key views instead of presenting a system
///     IME): NOT covered here — those need per-key tap dispatch via
///     the higher-level Orchestra layer.
///
/// Request args:
///   - `text` (required): the string to type.
///
/// Response data:
///   - `success`: always true — XCUITest has no per-character failure
///     signal, so apparent failure is indistinguishable from success
///     at this layer. The host adapter verifies post-condition
///     (field content) when it needs stronger guarantees.
///   - `typed`, `total`: both equal to `text.count`
///   - `reason`: "ok"
final class TypeTextCommand: CommandHandler {
    let type = "typeText"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let text = request.args["text"] as? String else {
            return .error(id: request.id, message: "missing text")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }
        bridge.typeText(app: app, text: text)
        return .ok(id: request.id, data: [
            "success": true,
            "typed": text.count,
            "total": text.count,
            "reason": "ok",
        ])
    }
}
