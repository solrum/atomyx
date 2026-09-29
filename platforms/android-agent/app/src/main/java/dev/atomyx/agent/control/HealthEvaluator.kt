package dev.atomyx.agent.control

/**
 * Facts /health needs to decide whether the accessibility service can
 * currently serve a request. Each fact is measured by the caller against
 * live framework state; a stale in-memory flag is not one of them, because
 * the system can leave the service instance in place while the binding it
 * depends on has already gone bad.
 */
data class HealthFacts(
    val serviceConnected: Boolean,
    val registeredWithSystem: Boolean,
    val canReadScreen: Boolean,
)

/** Fixed, caller-facing reason strings for a not-usable /health outcome. */
object HealthReasons {
    const val NOT_RUNNING =
        "The accessibility service is not running. Re-enable it on the device."
    const val CANNOT_READ_SCREEN =
        "The accessibility service is connected but cannot read the screen."
}

data class HealthDecision(
    val ok: Boolean,
    val accessibilityConnected: Boolean,
    val reason: String?,
)

object HealthEvaluator {

    /**
     * Classifies the observed facts into a /health response.
     *
     * A service that is not connected, or is connected but missing from the
     * platform's own list of running accessibility services, is reported the
     * same way as one that never started, because a caller cannot tell those
     * two apart from outside and both need the same remedy. A service that
     * is connected and registered but cannot enumerate windows is reported
     * separately, because the remedy differs: the binding itself is fine,
     * only the screen read is failing.
     */
    fun evaluate(facts: HealthFacts): HealthDecision {
        if (!facts.serviceConnected || !facts.registeredWithSystem) {
            return HealthDecision(
                ok = false,
                accessibilityConnected = false,
                reason = HealthReasons.NOT_RUNNING,
            )
        }
        if (!facts.canReadScreen) {
            return HealthDecision(
                ok = false,
                accessibilityConnected = true,
                reason = HealthReasons.CANNOT_READ_SCREEN,
            )
        }
        return HealthDecision(ok = true, accessibilityConnected = true, reason = null)
    }
}
