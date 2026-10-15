import Foundation

/// Type text into the currently-focused input field on the tracked app.
///
/// iOS handles typing natively via `XCUIApplication.typeText(_:)`, with
/// one call, whatever the system keyboard is showing. XCUITest
/// dispatches per-character events internally, including IME layout
/// switches, so callers do not need to tap individual keys.
///
/// The caller must have tapped (and thus focused) the target input
/// before calling. With nothing focused, nothing is typed and the reply
/// reports `success: false` with reason `no_focused_field`.
///
/// Framework notes (expand this list when adding support):
///
///   - Native UIKit / SwiftUI text inputs: accept `typeText` directly.
///   - Flutter Semantics text fields: accept `typeText` when a system
///     keyboard is presented.
///   - Custom in-app keyboards (e.g. RN or Flutter widgets that
///     render their own key views instead of presenting a system
///     IME): NOT covered here. Those need per-key tap dispatch via
///     the higher-level Orchestra layer.
///
/// Request args:
///   - `text` (required): the string to type.
///
/// Response data:
///   - `success`: true when the text was dispatched to a focused field,
///     false when no element held keyboard focus and nothing was typed.
///     XCUITest has no per-character failure signal, so the host adapter
///     verifies the field content when it needs stronger guarantees.
///   - `typed`: `text.count` on success, 0 otherwise
///   - `total`: `text.count`
///   - `reason`: "ok" on success, "no_focused_field" otherwise
final class TypeTextCommand: CommandHandler {
    let type = "typeText"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let text = request.args["text"] as? String else {
            return .error(id: request.id, message: "missing text")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched, call launchApp first")
        }
        guard bridge.typeText(app: app, text: text) else {
            return .ok(id: request.id, data: [
                "success": false,
                "typed": 0,
                "total": text.count,
                "reason": "no_focused_field",
            ])
        }
        return .ok(id: request.id, data: [
            "success": true,
            "typed": text.count,
            "total": text.count,
            "reason": "ok",
        ])
    }
}
