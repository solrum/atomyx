import Foundation
import XCTest

/// Default strategy for unknown keys. Types the raw key string into the
/// focused field via `XCUIApplication.typeText`. When no element holds
/// keyboard focus, nothing is typed and the result reports
/// `affordanceFound = false` with strategy `no_focused_field`. Registered as the
/// `fallback` of `PressKeyRegistry`, where strategies registered by name
/// win; everything else lands here.
///
/// `key` is set to an empty sentinel because `PressKeyRegistry` never
/// looks the fallback up by key. It's invoked from the registry's
/// `resolve` default path instead.
final class FallbackTypeStrategy: PressKeyStrategy {
    let key = ""
    let requiresApp = true

    func execute(key: String, app: XCUIApplication?) -> PressKeyResult {
        guard let app, appHasKeyboardFocus(app) else {
            return PressKeyResult(affordanceFound: false, strategy: noFocusedFieldStrategy)
        }
        app.typeText(key)
        return PressKeyResult(affordanceFound: true, strategy: "typed_raw")
    }
}
