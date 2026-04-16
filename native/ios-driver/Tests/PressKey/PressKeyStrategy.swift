import Foundation
import XCTest

/// Outcome of a press-key dispatch. `affordanceFound = true` means the
/// strategy used a verifiable control (nav bar back, home device press,
/// focused-field enter). `false` means a best-effort gesture was
/// dispatched but the driver cannot confirm it had any effect (e.g.
/// edge-swipe for iOS back gesture). The host adapter surfaces this as
/// `reason` text so agents can decide whether to fall back to a
/// screen-specific affordance.
struct PressKeyResult {
    let affordanceFound: Bool
    let strategy: String
}

/// One strategy per key intent. Strategies are the single place that
/// knows HOW a particular key is dispatched on iOS — `home` is
/// device-wide via `XCUIDevice`, `back` is a chain (nav bar →
/// edge-swipe), `enter` types a newline into the focused field, and
/// the registry's fallback handles unknown keys.
///
/// Strategies live in the adapter layer alongside `DefaultXCUIBridge`:
/// they call XCUI APIs directly. Unit tests inject `FakePressKeyStrategy`
/// instances into `PressKeyRegistry` instead of mocking the individual
/// XCUI calls.
///
/// Adding a new key (e.g. `siri`, `volume_up`, `lock`) is a drop-in:
/// create `SiriKeyStrategy` implementing this protocol, register it in
/// the composition root in `AtomyxDriverUITests.testServeCommands`. No
/// existing strategy or command needs to change. (OCP.)
protocol PressKeyStrategy {
    /// Wire key name this strategy responds to. `PressKeyRegistry`
    /// looks up by exact match. The fallback strategy registered via
    /// `PressKeyRegistry(fallback:)` handles unmatched keys and ignores
    /// this field.
    var key: String { get }

    /// `true` if `execute` needs a launched app reference. The command
    /// short-circuits with "no app launched" BEFORE calling execute
    /// when this is true and no app is tracked — so strategies can
    /// force-unwrap `app` when `requiresApp` is true.
    var requiresApp: Bool { get }

    /// Dispatch the key press. `key` is the raw wire key (useful for
    /// fallback strategies that need to know what to type). `app` is
    /// non-nil iff `requiresApp` is true.
    func execute(key: String, app: XCUIApplication?) -> PressKeyResult
}

/// Name-keyed lookup table for `PressKeyStrategy`. Registered strategies
/// are matched by exact `key`; unmatched keys route to the `fallback`
/// strategy (typically `FallbackTypeStrategy` which just types the raw
/// string into the focused field).
final class PressKeyRegistry {
    private var strategies: [String: PressKeyStrategy] = [:]
    private let fallback: PressKeyStrategy

    init(fallback: PressKeyStrategy) {
        self.fallback = fallback
    }

    func register(_ strategy: PressKeyStrategy) {
        strategies[strategy.key] = strategy
    }

    /// Resolve a key to its strategy. Never returns nil — unknown keys
    /// map to the fallback strategy passed at construction time.
    func resolve(_ key: String) -> PressKeyStrategy {
        strategies[key] ?? fallback
    }
}
