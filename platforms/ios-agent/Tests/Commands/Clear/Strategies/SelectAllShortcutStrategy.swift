import XCTest

/// ⌘A + ⌫ — O(1) keystrokes. Most effective path when the simulator
/// has hardware keyboard pairing enabled (default for CI and most
/// dev setups). Silently degrades on software-keyboard-only configs
/// where ⌘A is ignored; the chain falls through to the next strategy.
struct SelectAllShortcutStrategy: ClearTextStrategy {
    var name: String { "select-all-shortcut" }

    func attempt(context: ClearContext) throws -> ClearResult {
        guard let snapshot = try? context.app.snapshot(),
              let focused = findFocused(in: snapshot) else {
            return .skipped
        }
        let value = focused.value as? String ?? ""
        if value.isEmpty {
            return .success(strategy: "already-empty")
        }
        context.app.typeKey("a", modifierFlags: .command)
        context.app.typeKey(XCUIKeyboardKey.delete.rawValue, modifierFlags: [])
        return .attempted
    }
}
