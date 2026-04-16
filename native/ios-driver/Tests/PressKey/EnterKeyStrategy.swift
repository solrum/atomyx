import Foundation
import XCTest

/// `enter` = type a newline into whatever is focused. If no field is
/// focused, `typeText` is a silent no-op on iOS — we still report
/// `affordanceFound=true` because that matches the documented semantics
/// shared with Android (`enter` = best-effort submit) and agents
/// observe the outcome via state inspection after the call.
final class EnterKeyStrategy: PressKeyStrategy {
    let key = "enter"
    let requiresApp = true

    func execute(key _: String, app: XCUIApplication?) -> PressKeyResult {
        app!.typeText("\n")
        return PressKeyResult(affordanceFound: true, strategy: "enter")
    }
}
