package dev.atomyx.agent.control.clear

sealed class ClearResult {
    object Success : ClearResult()
    data class Failed(val reason: String) : ClearResult()
    object Skipped : ClearResult()
}
