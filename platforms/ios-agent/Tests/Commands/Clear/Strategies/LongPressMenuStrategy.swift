import XCTest

/// Long-press to trigger the system edit menu, then "Select All" + ⌫ (or "Cut").
/// Handles custom text inputs (Flutter GestureDetector grids, React Native
/// TextInput in some modes) that ignore triple-tap selection but still render
/// the iOS system context menu.
///
/// Vietnamese system keyboards expose "Chọn tất cả" instead of "Select All" —
/// both variants are checked so the strategy is locale-tolerant.
struct LongPressMenuStrategy: ClearTextStrategy {
    var name: String { "long-press-menu" }

    func attempt(context: ClearContext) throws -> ClearResult {
        let predicate = NSPredicate(format: "hasKeyboardFocus == TRUE")
        let element = context.app.descendants(matching: .any).matching(predicate).firstMatch
        guard element.exists else { return .skipped }

        element.press(forDuration: 0.8)

        // Poll up to 1.5 s for the system edit menu to appear.
        let deadline = Date().addingTimeInterval(1.5)
        while Date() < deadline {
            if context.app.menuItems["Select All"].exists {
                context.app.menuItems["Select All"].tap()
                context.app.typeKey(XCUIKeyboardKey.delete.rawValue, modifierFlags: [])
                return .attempted
            }
            // Locale fallback: Vietnamese system label.
            let selectAllButtons = context.app.buttons.matching(
                NSPredicate(format: "label CONTAINS[c] 'select all' OR label CONTAINS[c] 'chọn tất cả'")
            )
            if selectAllButtons.firstMatch.exists {
                selectAllButtons.firstMatch.tap()
                context.app.typeKey(XCUIKeyboardKey.delete.rawValue, modifierFlags: [])
                return .attempted
            }
            // "Cut" selects and removes all content in one action.
            if context.app.menuItems["Cut"].exists {
                context.app.menuItems["Cut"].tap()
                return .attempted
            }
            Thread.sleep(forTimeInterval: 0.1)
        }
        // No menu appeared — dismiss any partial interaction state and skip.
        context.app.tap()
        return .skipped
    }
}
