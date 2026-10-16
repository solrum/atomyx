/// Wire contract version this runner speaks. The host accepts only a
/// runner that reports exactly the version it declares, and the number
/// rises whenever the host and runner wire contract changes
/// incompatibly.
enum AgentProtocol {
    static let version = 3
}
