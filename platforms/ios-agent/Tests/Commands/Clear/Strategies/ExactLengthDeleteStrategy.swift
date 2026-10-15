import XCTest

/// Read the focused field's current value length and dispatch exactly that
/// many ⌫ keys, which is O(N) in content length but never over-deletes or under-deletes.
/// Capped by `ClearContext.hardCap` as a defensive ceiling against pathological
/// values that can't be read correctly via the snapshot API.
struct ExactLengthDeleteStrategy: ClearTextStrategy {
    var name: String { "exact-length-delete" }

    func attempt(context: ClearContext) throws -> ClearResult {
        guard let value = context.bridge.focusedFieldValue(app: context.app) else {
            return .skipped
        }
        let length = min(value.count, context.hardCap)
        guard length > 0 else { return .success(strategy: "already-empty") }
        let deleteStr = String(repeating: XCUIKeyboardKey.delete.rawValue, count: length)
        guard context.bridge.typeText(app: context.app, text: deleteStr) else { return .skipped }
        return .attempted
    }
}
