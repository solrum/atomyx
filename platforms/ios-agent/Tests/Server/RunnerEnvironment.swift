import Foundation

/// Launch parameters the runner reads from its process environment.
/// A launcher sets the `ATOMYX_*` variables on `xcodebuild` using the
/// `TEST_RUNNER_` prefix, which xcodebuild strips before handing them
/// to the test runner. `SIMULATOR_UDID` is set by Xcode itself for a
/// simulator destination and is never set for a physical device.
///
/// Every value except `port` is optional because a runner started by
/// hand carries no launcher identity.
struct RunnerEnvironment {
    static let defaultPort: UInt16 = 22087

    let port: UInt16
    let runnerId: String?
    let targetUdid: String?
    let simulatorUdid: String?

    /// An unset, empty, non-numeric, or out-of-range `ATOMYX_PORT`
    /// falls back to `defaultPort`, so a hand-started runner and the
    /// Studio flow keep their fixed port.
    init(environment: [String: String] = ProcessInfo.processInfo.environment) {
        if let raw = environment["ATOMYX_PORT"], let value = UInt16(raw), value > 0 {
            port = value
        } else {
            port = RunnerEnvironment.defaultPort
        }
        runnerId = RunnerEnvironment.nonEmpty(environment["ATOMYX_RUNNER_ID"])
        targetUdid = RunnerEnvironment.nonEmpty(environment["ATOMYX_TARGET_UDID"])
        simulatorUdid = RunnerEnvironment.nonEmpty(environment["SIMULATOR_UDID"])
    }

    private static func nonEmpty(_ value: String?) -> String? {
        guard let value, !value.isEmpty else { return nil }
        return value
    }
}
