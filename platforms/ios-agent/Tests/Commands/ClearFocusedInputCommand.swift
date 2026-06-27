import Foundation
import XCTest

/// Clear the currently-focused text field in XCUITest using a 4-strategy
/// priority chain. Strategies are tried in order until one succeeds or all
/// are exhausted.
///
/// Chain order (fastest → most universal):
///
///   1. SelectAllShortcutStrategy  — ⌘A + ⌫ (O(1) keystrokes).
///   2. TripleTapStrategy          — triple-tap select + ⌫.
///   3. LongPressMenuStrategy      — long-press system edit menu → "Select All"/"Cut".
///   4. ExactLengthDeleteStrategy  — reads value length, sends exactly N ⌫ keys.
///
/// After each `.attempted` result the chain polls the focused field (up to 500 ms)
/// to confirm the value is empty before advancing. A strategy returning `.success`
/// bypasses the verify gate (it already confirmed the field is empty).
///
/// Request args:
///   - `maxDeletes` (optional, default 500): upper bound for ExactLengthDeleteStrategy.
///     Strategies 1–3 ignore this arg.
///
/// Response data (success):
///   - `strategy`: name of the winning strategy path.
///
/// Response data (failure): encoded in the error message as a JSON object with:
///   - `code`: "clear-text-failed"
///   - `strategiesTried`: array of strategy names attempted before failure.
///   - `lastValue`: the field value observed after all strategies, or null.
///   - `focusedElementType`: XCUIElement type string of the focused element.
///   - `hasHardwareKeyboard`: true when no on-screen system keyboard was visible.
final class ClearFocusedInputCommand: CommandHandler {
    let type = "clearFocusedInput"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let hardCap = max(0, min((request.args["maxDeletes"] as? Int) ?? 500, 500))
        let chain = ClearTextChain(strategies: [
            SelectAllShortcutStrategy(),
            TripleTapStrategy(),
            LongPressMenuStrategy(),
            ExactLengthDeleteStrategy(),
        ])
        let ctx = ClearContext(app: app, bridge: bridge, hardCap: hardCap)

        do {
            let strategy = try chain.run(context: ctx)
            return .ok(id: request.id, data: ["strategy": strategy])
        } catch ClearTextChainError.allStrategiesFailed(let diagnostic) {
            let diagnosticDict: [String: Any] = [
                "code": "clear-text-failed",
                "strategiesTried": diagnostic.strategiesTried,
                "lastValue": diagnostic.lastValue as Any,
                "focusedElementType": diagnostic.focusedElementType,
                "hasHardwareKeyboard": diagnostic.hasHardwareKeyboard,
            ]
            let jsonString: String
            if let data = try? JSONSerialization.data(withJSONObject: diagnosticDict),
               let str = String(data: data, encoding: .utf8) {
                jsonString = str
            } else {
                jsonString = "clear-text-failed"
            }
            return .error(id: request.id, message: jsonString)
        } catch {
            return .error(id: request.id, message: "clearFocusedInput: unexpected error: \(error)")
        }
    }
}
