package dev.atomyx.agent.control

/**
 * Wire contract version this agent speaks. The host accepts only an
 * agent that reports exactly the version it declares, and the number
 * rises whenever the host and agent wire contract changes
 * incompatibly.
 */
object AgentProtocol {
    const val AGENT_PROTOCOL_VERSION = 3
}
