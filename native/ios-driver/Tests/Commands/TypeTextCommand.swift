import Foundation

/// Type text into the currently-focused input field on the tracked app.
///
/// Unlike Android's `typeViaKeyboard` — which taps each key in turn to
/// deal with IME layout switching on custom keyboards — iOS handles
/// typing natively via `XCUIApplication.typeText(_:)`. One call,
/// whatever the system keyboard is showing. Custom in-app keyboards
/// (Flutter / React Native widgets that render their own key views)
/// will NOT pick up `typeText` and need per-key taps instead; that
/// fallback path is deferred to Batch 3.
///
/// The caller must have tapped (and thus focused) the target input
/// before calling this command. `typeText` with nothing focused is a
/// silent no-op on iOS — XCUITest swallows the event. The host adapter
/// is responsible for ensuring focus.
///
/// Request args:
///   - `text` (required): the string to type.
///
/// Response data mirrors `DeviceActor.typeViaKeyboard` return shape so
/// the host adapter can pass it through without reshaping:
///   - `success`: always true (iOS has no per-character failure signal)
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
