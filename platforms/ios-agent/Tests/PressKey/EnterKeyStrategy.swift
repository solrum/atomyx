import Foundation
import XCTest

/// `enter` = type a newline into whatever is focused. If no field is
/// focused, nothing is typed and the result reports
/// `affordanceFound=false` with strategy `no_focused_field`, because
/// typing without a focused element records an XCTest failure that would
/// end the runner.
final class EnterKeyStrategy: PressKeyStrategy {
    let key = "enter"
    let requiresApp = true

    func execute(key _: String, app: XCUIApplication?) -> PressKeyResult {
        guard let app, appHasKeyboardFocus(app) else {
            return PressKeyResult(affordanceFound: false, strategy: noFocusedFieldStrategy)
        }
        app.typeText("\n")
        return PressKeyResult(affordanceFound: true, strategy: "enter")
    }
}
