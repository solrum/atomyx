package dev.atomyx.agent.control

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Pure-function tests for `HealthEvaluator.evaluate`. Each case pins one
 * combination of observed facts to one /health outcome, so a regression
 * that lets a stale-but-unusable service report itself healthy surfaces as
 * a named failure.
 */
class HealthEvaluatorTest {

    @Test
    fun everyFactTrueReportsHealthy() {
        val decision = HealthEvaluator.evaluate(
            HealthFacts(serviceConnected = true, registeredWithSystem = true, canReadScreen = true),
        )
        assertEquals(HealthDecision(ok = true, accessibilityConnected = true, reason = null), decision)
    }

    @Test
    fun noServiceInstanceReportsNotRunning() {
        val decision = HealthEvaluator.evaluate(
            HealthFacts(serviceConnected = false, registeredWithSystem = false, canReadScreen = false),
        )
        assertEquals(
            HealthDecision(ok = false, accessibilityConnected = false, reason = HealthReasons.NOT_RUNNING),
            decision,
        )
    }

    @Test
    fun connectedInstanceMissingFromSystemListReportsNotRunning() {
        // The published instance did not clear itself, but the platform no
        // longer counts it among the running accessibility services. This
        // is the "Crashed services" shape observed on-device: the process
        // stayed alive while the binding underneath it went bad.
        val decision = HealthEvaluator.evaluate(
            HealthFacts(serviceConnected = true, registeredWithSystem = false, canReadScreen = true),
        )
        assertEquals(
            HealthDecision(ok = false, accessibilityConnected = false, reason = HealthReasons.NOT_RUNNING),
            decision,
        )
    }

    @Test
    fun connectedAndRegisteredButCannotReadScreenReportsScreenReadFailure() {
        val decision = HealthEvaluator.evaluate(
            HealthFacts(serviceConnected = true, registeredWithSystem = true, canReadScreen = false),
        )
        assertEquals(
            HealthDecision(ok = false, accessibilityConnected = true, reason = HealthReasons.CANNOT_READ_SCREEN),
            decision,
        )
    }

    @Test
    fun reasonStringsCarryNoRouteOrPortFragments() {
        // The Go host sanitizes anything shaped like a path or a
        // "colon followed by digits" out of a diagnostic message before
        // showing it to a caller, so these fixed strings must never
        // contain either shape.
        val portLikeFragment = Regex(":\\d")
        val pathLikeFragment = Regex("/")
        for (reason in listOf(HealthReasons.NOT_RUNNING, HealthReasons.CANNOT_READ_SCREEN)) {
            assertEquals(false, portLikeFragment.containsMatchIn(reason))
            assertEquals(false, pathLikeFragment.containsMatchIn(reason))
        }
    }
}
