import XCTest

/// Read the focused field's current value length and dispatch exactly that
/// many ⌫ keys — O(N) in content length but never over-deletes or under-deletes.
/// Capped by `ClearContext.hardCap` as a defensive ceiling against pathological
/// values that can't be read correctly via the snapshot API.
struct ExactLengthDeleteStrategy: ClearTextStrategy {
    var name: String { "exact-length-delete" }

    func attempt(context: ClearContext) throws -> ClearResult {
        guard let snapshot = try? context.app.snapshot(),
              let focused = findFocused(in: snapshot) else {
            return .skipped
        }
        let value = focused.value as? String ?? ""
        let length = min(value.count, context.hardCap)
        guard length > 0 else { return .success(strategy: "already-empty") }
        let deleteStr = String(repeating: XCUIKeyboardKey.delete.rawValue, count: length)
        context.bridge.typeText(app: context.app, text: deleteStr)
        return .attempted
    }
}
