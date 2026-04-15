import Foundation
import XCTest

/// Clear the currently-focused text field by typing a run of delete
/// keys. iOS does not expose a direct "clear text" primitive on
/// `XCUIApplication` (only `XCUIElement.typeText(delete)` on a
/// specific element reference, which would require knowing which
/// element is focused and costs extra RPCs).
///
/// Implementation: type `XCUIKeyboardKey.delete.rawValue` N times via
/// the existing `typeText` bridge path. 100 repeats is enough for all
/// practical form fields (phone numbers, email, short passwords);
/// longer content should use `inputText(selector, "")` via explicit
/// select-all + delete semantics which is deferred to Batch 3.
///
/// Caveat: this is a silent no-op if no field is focused (XCUITest
/// swallows keyboard input with no target). The caller is responsible
/// for ensuring focus before calling — typically by tapping the target
/// input first via `tap(selector)`.
///
/// Request args:
///   - `maxDeletes` (optional, default 100): number of delete keys to
///     send. Caps prevent runaway repeat counts on edge cases.
///
/// Response data:
///   - `deleted`: how many delete keys were sent.
final class ClearFocusedInputCommand: CommandHandler {
    let type = "clearFocusedInput"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let rawCount = (request.args["maxDeletes"] as? Int) ?? 100
        let count = max(0, min(rawCount, 500)) // hard cap to avoid runaway
        let deleteChar = XCUIKeyboardKey.delete.rawValue
        let payload = String(repeating: deleteChar, count: count)
        bridge.typeText(app: app, text: payload)

        return .ok(id: request.id, data: ["deleted": count])
    }
}
