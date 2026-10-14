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
///
/// Reports `protocolVersion`, the wire contract version from
/// `AgentProtocol`, which the host compares with its own to decide
/// whether it can drive this runner. Reports `agentVersion`, the runner
/// bundle's `CFBundleShortVersionString`, for display only. The
/// `agentVersion` field is omitted when the bundle declares none.
final class PingCommand: CommandHandler {
    let type = "ping"

    private let synthesizer: EventSynthesizer
    private let environment: RunnerEnvironment
    private let agentVersion: String?

    init(
        synthesizer: EventSynthesizer,
        environment: RunnerEnvironment = RunnerEnvironment(),
        agentVersion: String? = PingCommand.bundleVersion()
    ) {
        self.synthesizer = synthesizer
        self.environment = environment
        self.agentVersion = agentVersion
    }

    private static func bundleVersion() -> String? {
        let value = Bundle(for: PingCommand.self).infoDictionary?["CFBundleShortVersionString"] as? String
        guard let value, !value.isEmpty else { return nil }
        return value
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
            "protocolVersion": AgentProtocol.version,
            "capabilities": capsDict,
            // Mechanism name is internal telemetry; the host
            // correlates it with logs but never branches on it
            // (capability matrix is the contract).
            "mechanism": synthesizer.mechanismName,
        ]
        if let agentVersion {
            data["agentVersion"] = agentVersion
        }
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
