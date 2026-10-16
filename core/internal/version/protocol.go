package version

// AgentProtocolVersion is the wire contract version between the host
// and the native agents. The host accepts only an agent that reports
// exactly this version. The number rises whenever the host and agent
// wire contract changes incompatibly, and the Android agent and the
// iOS runner carry the same number in their own sources.
const AgentProtocolVersion = 3
