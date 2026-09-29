package dev.atomyx.agent.control

import java.lang.reflect.InvocationTargetException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Pure-function tests for
 * `SystemActionDispatcher.classifyStopAttempt`. Each case pins one
 * outcome of a force-stop attempt so a regression surfaces as a named
 * failure rather than a silent success report for a stop the platform
 * actually refused.
 *
 * These tests run without an Android runtime: `classifyStopAttempt`
 * takes the `Throwable?` a stop attempt threw and never touches
 * `AccessibilityService` / `ActivityManager`.
 */
class SystemActionDispatcherClassifyStopAttemptTest {

    @Test
    fun nothingThrownIsTreatedAsStopped() {
        val result = SystemActionDispatcher.classifyStopAttempt("com.example.app", null)
        assertEquals(TapResult(true, "stopped com.example.app"), result)
    }

    @Test
    fun directSecurityExceptionIsRefused() {
        val result = SystemActionDispatcher.classifyStopAttempt(
            "com.example.app",
            SecurityException("Neither user 10123 nor current process has android.permission.KILL_BACKGROUND_PROCESSES"),
        )
        assertEquals(
            TapResult(false, "Not permitted to stop com.example.app."),
            result,
        )
    }

    @Test
    fun securityExceptionWrappedInInvocationTargetExceptionIsRefused() {
        // `Method.invoke` wraps whatever the target method throws, so a
        // reflective `forceStopPackage` call surfaces its
        // SecurityException this way, not directly.
        val wrapped = InvocationTargetException(
            SecurityException("Permission Denial: not allowed to send broadcast"),
        )
        val result = SystemActionDispatcher.classifyStopAttempt("com.example.app", wrapped)
        assertEquals(
            TapResult(false, "Not permitted to stop com.example.app."),
            result,
        )
    }

    @Test
    fun unrelatedExceptionFallsThroughToNextStrategy() {
        val result = SystemActionDispatcher.classifyStopAttempt(
            "com.example.app",
            IllegalStateException("unexpected"),
        )
        assertNull("a non-permission failure must fall through, not report a definite outcome", result)
    }
}
