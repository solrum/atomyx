import XCTest

/// Triple-tap to select all text in the focused element, then ⌫.
/// Fallback for software-keyboard configs where ⌘A is silently ignored.
/// Works when the focused element is tappable and exposes a text selection
/// affordance (standard UITextField / UITextView; not all custom inputs).
struct TripleTapStrategy: ClearTextStrategy {
    var name: String { "triple-tap" }

    func attempt(context: ClearContext) throws -> ClearResult {
        let predicate = NSPredicate(format: "hasKeyboardFocus == TRUE")
        let element = context.app.descendants(matching: .any).matching(predicate).firstMatch
        guard element.exists else { return .skipped }
        element.tap(withNumberOfTaps: 3, numberOfTouches: 1)
        context.app.typeKey(XCUIKeyboardKey.delete.rawValue, modifierFlags: [])
        return .attempted
    }
}
