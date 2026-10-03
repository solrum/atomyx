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
/// host.
///
/// Reports the launch identity (`runnerId`, `targetUdid`,
/// `simulatorUdid`) so the host can tell its own runner apart from
/// another runner answering on the same port. A field is present only
/// when the launch environment supplied it.
final class PingCommand: CommandHandler {
    let type = "ping"

    private let synthesizer: EventSynthesizer
    private let environment: RunnerEnvironment

    init(synthesizer: EventSynthesizer, environment: RunnerEnvironment = RunnerEnvironment()) {
        self.synthesizer = synthesizer
        self.environment = environment
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
        if let runnerId = environment.runnerId {
            data["runnerId"] = runnerId
        }
        if let targetUdid = environment.targetUdid {
            data["targetUdid"] = targetUdid
        }
        if let simulatorUdid = environment.simulatorUdid {
            data["simulatorUdid"] = simulatorUdid
        }
        if let log = synthesizer.probeLog {
            data["probeLog"] = log
        }
        return .ok(id: request.id, data: data)
    }
}
