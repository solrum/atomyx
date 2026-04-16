import Foundation

/// Liveness probe. Used by the host adapter to verify the driver is
/// actually serving on the port after `xcodebuild test-without-building`
/// reports test start. Returns the process id for correlation with
/// xcodebuild logs on failure.
final class PingCommand: CommandHandler {
    let type = "ping"

    func handle(_ request: Request, bridge _: XCUIBridge, state _: DriverState) -> Response {
        .ok(id: request.id, data: [
            "pong": true,
            "pid": ProcessInfo.processInfo.processIdentifier,
        ])
    }
}
