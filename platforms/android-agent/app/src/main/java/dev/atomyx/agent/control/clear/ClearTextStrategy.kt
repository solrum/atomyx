package dev.atomyx.agent.control.clear

interface ClearTextStrategy {
    val name: String
    fun attempt(context: ClearContext): ClearResult
}
