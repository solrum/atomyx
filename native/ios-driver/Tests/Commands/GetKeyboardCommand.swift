import Foundation

/// Query the system keyboard state. Used by:
///   - `ImeGeometricGuard` at the tool layer to reject taps inside
///     the keyboard window (prevents coordinate-reset tricks and
///     accidental key presses)
///   - Agent reasoning about "is the keyboard visible" state when
///     deciding whether to type directly or dismiss first
///
/// Limitations:
///   - Only detects system `UIKeyboard` windows. Custom in-app
///     keyboards (Flutter GestureDetector grids, RN TouchableOpacity
///     key views) are regular app views and return `visible=false`.
///     Agent must handle the "focused field + invisible keyboard"
///     case via dumpTree inspection (Phase 6 hardening).
///   - Layout classification is heuristic, not exact. See
///     `DefaultXCUIBridge.detectKeyboardLayout`.
///
/// Response data:
///   - `visible`: true when a system keyboard window exists
///   - `layout`: heuristic string ("qwerty", "numeric_pad", "unknown",
///     "none"). Mirrors the Android `KeyboardInfo.layout` enum values
///   - `bounds`: keyboard window frame in points, or null when not visible
///   - `keys`: array of `{label, bounds}` per key; empty when not visible
final class GetKeyboardCommand: CommandHandler {
    let type = "getKeyboard"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let info = bridge.getKeyboard(app: app)
        if !info.visible {
            return .ok(id: request.id, data: [
                "visible": false,
                "layout": "none",
                "bounds": NSNull(),
                "keys": [] as [Any],
            ])
        }

        let keyDicts: [[String: Any]] = info.keys.map { k in
            [
                "label": k.label,
                "bounds": boundsDict(from: k.frame),
            ]
        }
        return .ok(id: request.id, data: [
            "visible": true,
            "layout": info.layout,
            "bounds": boundsDict(from: info.frame),
            "keys": keyDicts,
        ])
    }

    private func boundsDict(from rect: CGRect) -> [String: Int] {
        [
            "left": Int(rect.minX),
            "top": Int(rect.minY),
            "right": Int(rect.maxX),
            "bottom": Int(rect.maxY),
        ]
    }
}
