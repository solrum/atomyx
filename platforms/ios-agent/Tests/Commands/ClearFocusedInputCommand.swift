import Foundation
import XCTest

/// Clear the currently-focused text field in XCUITest. Two-stage
/// strategy that keeps the common case O(1) in keystrokes while
/// still handling pathological configurations.
///
/// Stage 1 — ⌘A + ⌫ (O(1) keystrokes).
///   Fastest path when the simulator has hardware-keyboard pairing
///   on (default for CI and most dev setups). Two XCUITest key
///   events wipe the field regardless of its content length.
///
/// Stage 2 — exact-length delete loop (O(N) keystrokes).
///   Fires only when Stage 1 didn't clear. Reads the focused
///   element's current value length from `app.snapshot()` and
///   dispatches exactly that many ⌫ keys — no over-delete, no
///   under-delete. The count is upper-bounded by `maxDeletes`
///   (default 500) as a defensive cap against pathological values
///   that can't be read correctly; in practice the read length
///   governs.
///
/// Early-out: when the focused field is already empty (or its
/// displayed value equals its accessibility label — Flutter's
/// placeholder-as-value mirror), both stages are skipped. The
/// Orchestra layer already avoids calling this on an empty field;
/// the agent-side no-op keeps the contract honest for direct
/// callers too.
///
/// Caveat: silent no-op when no field is focused (XCUITest
/// swallows keyboard input with no target). Caller must ensure
/// focus first via tap.
///
/// Request args:
///   - `maxDeletes` (optional, default 500): defensive upper bound
///     on the Stage 2 loop. Stage 1 ignores this.
///
/// Response data:
///   - `strategy`: which path fired — "already-empty",
///     "select-all-delete", or "delete-loop:<count>".
final class ClearFocusedInputCommand: CommandHandler {
    let type = "clearFocusedInput"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let hardCap = max(0, min((request.args["maxDeletes"] as? Int) ?? 500, 500))

        // Observe current value. If the focused field is already
        // empty (or shows only its placeholder), skip entirely.
        let before = focusedValueLength(app: app)
        if before == 0 {
            return .ok(id: request.id, data: ["strategy": "already-empty"])
        }

        // Stage 1: ⌘A + ⌫.
        _ = bridge.typeKey(app: app, key: "a", modifiers: .command)
        bridge.typeText(app: app, text: String(XCUIKeyboardKey.delete.rawValue))

        // Verify Stage 1 worked. Some Flutter TextField configs
        // ignore ⌘A (software keyboard only, or unpaired simulator
        // hardware keyboard). When the value didn't shrink to zero,
        // fall back to an exact-length delete loop.
        let afterStage1 = focusedValueLength(app: app)
        if afterStage1 == 0 {
            return .ok(id: request.id, data: ["strategy": "select-all-delete"])
        }

        let deleteCount = min(afterStage1, hardCap)
        if deleteCount > 0 {
            let payload = String(
                repeating: XCUIKeyboardKey.delete.rawValue,
                count: deleteCount
            )
            bridge.typeText(app: app, text: payload)
        }
        return .ok(
            id: request.id,
            data: ["strategy": "delete-loop:\(deleteCount)"]
        )
    }

    /// Count the character length of the focused field's current
    /// value. Returns 0 when no focus is resolved, when the value is
    /// missing, or when the value happens to equal the field's
    /// accessibility label (Flutter's placeholder-as-value mirror).
    /// The cross-bridge read keeps the agent the single source of
    /// truth for the "how much is in the field right now" question —
    /// the host doesn't need to round-trip the hierarchy just to
    /// decide how many deletes to send.
    private func focusedValueLength(app: XCUIApplication) -> Int {
        guard let snapshot = try? app.snapshot() else { return 0 }
        var stack: [XCUIElementSnapshot] = [snapshot]
        while let node = stack.popLast() {
            if node.hasFocus {
                let value = (node.value as? String) ?? ""
                if value.isEmpty { return 0 }
                if value == node.label { return 0 }
                return value.count
            }
            for child in node.children.reversed() {
                stack.append(child)
            }
        }
        return 0
    }
}
