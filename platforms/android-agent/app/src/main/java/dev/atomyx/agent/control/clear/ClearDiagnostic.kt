package dev.atomyx.agent.control.clear

data class ClearDiagnostic(
    val strategiesTried: List<String>,
    val lastValue: String,
    val focusedNodeDesc: String,
    val screenWidth: Int,
    val screenHeight: Int,
)
