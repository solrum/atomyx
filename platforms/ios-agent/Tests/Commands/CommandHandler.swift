import Foundation
import XCTest

/// Mutable driver state shared across commands. Holds the currently
/// launched app reference — XCUITest has no system-wide foreground
/// query, so the driver tracks this itself. Without it, every
/// subsequent command would need its own `XCUIApplication` lookup
/// and most of them cannot even perform one.
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

    /// Threading contract. `true` (default) — CommandServer wraps
    /// the handler in `DispatchQueue.main.sync` so it runs on the
    /// main thread (required by most XCUITest APIs, including
    /// `XCUIApplication.launch()`). `false` — the handler runs
    /// on the server's background accept queue; suitable for
    /// commands that MUST have the main queue free (e.g.
    /// `DispatchPointerCommand`, whose `synthesizeEvent:completion:`
    /// completion block is posted to main and would deadlock if
    /// main were blocked).
    var requiresMainThread: Bool { get }

    /// Dispatch entry point. Must return a `Response` matching the
    /// request id. May throw only `XCTSkip` — other errors should
    /// become `.error` responses for the wire.
    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response
}

extension CommandHandler {
    var requiresMainThread: Bool { true }
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

    /// Threading hint for `CommandServer`. Unknown commands
    /// default to `true` — they'll be routed onto main and hit
    /// the "unknown command" error branch there, which is
    /// harmless.
    func requiresMainThread(for type: String) -> Bool {
        return handlers[type]?.requiresMainThread ?? true
    }
}
