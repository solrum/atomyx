import Foundation
import XCTest

/// Default strategy for unknown keys. Types the raw key string into the
/// focused field via `XCUIApplication.typeText`. Registered as the
/// `fallback` of `PressKeyRegistry` — strategies registered by name
/// win; everything else lands here.
///
/// `key` is set to an empty sentinel because `PressKeyRegistry` never
/// looks the fallback up by key — it's invoked from the registry's
/// `resolve` default path.
final class FallbackTypeStrategy: PressKeyStrategy {
    let key = ""
    let requiresApp = true

    func execute(key: String, app: XCUIApplication?) -> PressKeyResult {
        app!.typeText(key)
        return PressKeyResult(affordanceFound: true, strategy: "typed_raw")
    }
}
