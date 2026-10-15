package dev.atomyx.agent.control.clear

/**
 * Result of a clear attempt. [diagnostic] is present exactly when the
 * attempt failed, so a caller can report why the field stayed non-empty.
 */
data class ClearOutcome(
    val success: Boolean,
    val reason: String,
    val diagnostic: ClearDiagnostic?,
) {
    companion object {
        fun succeeded(reason: String) = ClearOutcome(true, reason, null)

        fun failed(reason: String, diagnostic: ClearDiagnostic) =
            ClearOutcome(false, reason, diagnostic)
    }
}

/**
 * Wire reply for the clear route. `ok` and `reason` are always sent. A
 * failed outcome adds the strategies tried, the value left in the field,
 * a description of the focused node, and the screen size in pixels.
 */
fun clearReplyPayload(outcome: ClearOutcome): Map<String, Any> {
    val payload = linkedMapOf<String, Any>(
        "ok" to outcome.success,
        "reason" to outcome.reason,
    )
    outcome.diagnostic?.let {
        payload["strategiesTried"] = it.strategiesTried
        payload["lastValue"] = it.lastValue
        payload["focusedNodeDesc"] = it.focusedNodeDesc
        payload["screenWidth"] = it.screenWidth
        payload["screenHeight"] = it.screenHeight
    }
    return payload
}
