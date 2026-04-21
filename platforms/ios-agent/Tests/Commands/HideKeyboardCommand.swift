import Foundation

/// Dismiss the on-screen keyboard if one is visible. Idempotent —
/// returns `ok: false, strategy: "not-visible"` when no keyboard is
/// up, which the host treats as "already done".
///
/// The bridge tries a strategy chain (dismiss affordance → tap
/// above keyboard). Which one fires is reported back via `strategy`
/// so agent logs can tell the difference between "clean dismiss
/// via button" and "best-effort tap outside".
///
/// This command dispatches the gesture and returns immediately. It
/// does NOT wait for the keyboard to finish animating off-screen.
/// Callers that need the keyboard confirmed gone use the host-side
/// `waitForKeyboard(false)` primitive over `hierarchy()`.
///
/// Request args: none.
///
/// Response data:
///   - `ok`: true when a dismissal gesture was dispatched.
///   - `strategy`: which strategy fired ("dismiss-affordance:<label>",
///     "tap-above-keyboard", or "not-visible").
final class HideKeyboardCommand: CommandHandler {
    let type = "hideKeyboard"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }
        let result = bridge.hideKeyboard(app: app)
        return .ok(id: request.id, data: [
            "ok": result.ok,
            "strategy": result.strategy,
        ])
    }
}
