import Foundation

/// Liveness probe. Used by the host adapter to verify the driver
/// is actually serving on the port after
/// `xcodebuild test-without-building` reports test start. Returns
/// the process id for correlation with xcodebuild logs on failure.
///
/// Also surfaces the gesture capability matrix so the host can
/// populate `Driver.capabilities` without a second RPC: the YAML
/// validator reads `canMultiPointer` / `canPressure` at dispatch
/// time to reject unsupported gesture shapes before they leave the
/// TypeScript side.
final class PingCommand: CommandHandler {
    let type = "ping"

    private let synthesizer: EventSynthesizer

    init(synthesizer: EventSynthesizer) {
        self.synthesizer = synthesizer
    }

    func handle(_ request: Request, bridge _: XCUIBridge, state _: DriverState) -> Response {
        let caps = synthesizer.capabilities
        let capsDict: [String: Any] = [
            "canMultiPointer": caps.canMultiPointer,
            "canPressure": caps.canPressure,
        ]
        var data: [String: Any] = [
            "pong": true,
            "pid": ProcessInfo.processInfo.processIdentifier,
            "capabilities": capsDict,
            // Mechanism name is internal telemetry; the host
            // correlates it with logs but never branches on it
            // (capability matrix is the contract).
            "mechanism": synthesizer.mechanismName,
        ]
        if let log = synthesizer.probeLog {
            data["probeLog"] = log
        }
        return .ok(id: request.id, data: data)
    }
}
