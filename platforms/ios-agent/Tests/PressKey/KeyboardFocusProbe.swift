import XCTest

/// Reports whether any element in the app currently holds keyboard focus.
///
/// `XCUIApplication.typeText` records an XCTest failure, not a thrown
/// error, when no element has focus. A recorded failure ends the serve
/// test and the runner with it, so every typing path checks this probe
/// first and degrades to a not-performed result instead.
///
/// `firstMatch.exists` stops at the first focused element, whereas a
/// `count` would resolve every match. A keyboard-presence pre-filter is
/// not equivalent because focus also exists with a hardware keyboard.
func appHasKeyboardFocus(_ app: XCUIApplication) -> Bool {
    let predicate = NSPredicate(format: "hasKeyboardFocus == TRUE")
    return app.descendants(matching: .any).matching(predicate).firstMatch.exists
}

/// Strategy name reported when a key press needs a focused field and
/// none exists.
let noFocusedFieldStrategy = "no_focused_field"
