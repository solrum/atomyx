import Foundation
import XCTest

/// Mutable driver state shared across commands. Holds the currently
/// launched app reference — XCUITest has no system-wide foreground
/// query, so the driver tracks this itself. See `.claude/docs/ios.md`
/// for the rationale.
///
/// **Thread safety:** `DriverState` is NOT internally synchronized. It
/// relies on `CommandServer` serializing all handler dispatches on the
/// main thread via `DispatchQueue.main.sync` (see
/// `CommandServer.serveClient`) — XCUITest API calls also require the
/// main thread, so the two constraints happen to coincide. If
/// `CommandServer` is ever changed to fan out work to multiple queues,
/// this class must gain a lock or be refactored into an actor.
final class DriverState {
    var currentApp: XCUIApplication?
    var currentBundleId: String = ""
}

/// One per command. Analogue of Android's `Route` protocol.
protocol CommandHandler {
    /// Wire protocol `type` field that this handler responds to.
    var type: String { get }

    /// Dispatch entry point. Must return a `Response` matching the
    /// request id. May throw only `XCTSkip` — other errors should
    /// become `.error` responses for the wire.
    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response
}

/// Registry + dispatcher. Commands are registered once at startup in
/// the test entry point, then looked up by `type` on each request.
/// Unknown commands return a structured error.
final class CommandRegistry {
    private var handlers: [String: CommandHandler] = [:]

    func register(_ handler: CommandHandler) {
        handlers[handler.type] = handler
    }

    func dispatch(
        _ request: Request,
        bridge: XCUIBridge,
        state: DriverState
    ) -> Response {
        guard let handler = handlers[request.type] else {
            return .error(id: request.id, message: "unknown command: \(request.type)")
        }
        return handler.handle(request, bridge: bridge, state: state)
    }
}
