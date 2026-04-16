import Foundation
import XCTest

/// Press a system or keyboard key via a strategy registered in
/// `PressKeyRegistry`. The command is pure orchestration:
///
///   1. Validate the `key` argument.
///   2. Resolve the strategy (registered name → fallback).
///   3. Guard `state.currentApp` iff `strategy.requiresApp`.
///   4. Dispatch `strategy.execute(key:app:)` and forward the result
///      onto the wire.
///
/// All key-specific logic (home/enter/back chain/typed-raw) lives in
/// per-strategy files under `Tests/PressKey/`. Adding a new key = drop
/// in a new strategy file + register in the composition root. This
/// command never needs to change again.
///
/// Response data:
///   - `key`: echoed back for correlation
///   - `affordanceFound`: true iff a verifiable control was used
///   - `strategy`: which path was taken (nav_bar_back | edge_swipe_best_effort
///                 | home | enter | typed_raw | ...)
final class PressKeyCommand: CommandHandler {
    let type = "pressKey"
    private let registry: PressKeyRegistry

    init(registry: PressKeyRegistry) {
        self.registry = registry
    }

    func handle(_ request: Request, bridge _: XCUIBridge, state: DriverState) -> Response {
        guard let key = request.args["key"] as? String, !key.isEmpty else {
            return .error(id: request.id, message: "missing key")
        }

        let strategy = registry.resolve(key)

        let app: XCUIApplication?
        if strategy.requiresApp {
            guard let tracked = state.currentApp else {
                return .error(id: request.id, message: "no app launched — call launchApp first")
            }
            app = tracked
        } else {
            app = nil
        }

        let result = strategy.execute(key: key, app: app)
        return .ok(id: request.id, data: [
            "key": key,
            "affordanceFound": result.affordanceFound,
            "strategy": result.strategy,
        ])
    }
}
