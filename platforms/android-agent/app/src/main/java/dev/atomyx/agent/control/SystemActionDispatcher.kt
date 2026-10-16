package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.util.Log
import java.lang.reflect.InvocationTargetException

/**
 * Global / OS-scope actions that don't touch the gesture pipeline:
 * navigation keys (back / home), app launch, force-stop.
 *
 * Kept separate from [GestureDispatcher] and the keyboard/input
 * classes because these paths route through `performGlobalAction`
 * or `Context` directly. They never build a `GestureDescription`
 * or read the focused-node tree.
 */
class SystemActionDispatcher(
    private val service: AccessibilityService,
) {

    /** Outcome of a press_key request. */
    sealed class KeyPressOutcome {
        object Dispatched : KeyPressOutcome()

        /** The caller-supplied key name is not one this agent recognizes. */
        data class Unsupported(val key: String) : KeyPressOutcome()
    }

    fun pressKey(key: String): KeyPressOutcome {
        val action = KEY_ACTIONS[key] ?: return KeyPressOutcome.Unsupported(key)
        service.performGlobalAction(action)
        return KeyPressOutcome.Dispatched
    }

    /** Outcome of a launch request. */
    sealed class LaunchOutcome {
        object Launched : LaunchOutcome()

        /** The caller-supplied package has no activity the platform will launch. */
        data class NoLaunchableActivity(val packageName: String) : LaunchOutcome()
    }

    /**
     * Starts [packageName] through its launcher activity. Each entry of
     * [arguments] is put on the launch intent as a string extra, so the
     * app reads it from its launching intent at startup. The extras
     * reach the app only when the launch creates its task, which is why
     * the host stops a running app before a launch that carries them.
     */
    fun launchApp(packageName: String, arguments: Map<String, String> = emptyMap()): LaunchOutcome {
        val intent = service.packageManager.getLaunchIntentForPackage(packageName)
            ?: return LaunchOutcome.NoLaunchableActivity(packageName)
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        arguments.forEach { (name, value) -> intent.putExtra(name, value) }
        service.startActivity(intent)
        return LaunchOutcome.Launched
    }

    /**
     * Force-stop an app package.
     *
     * Neither strategy this agent can reach without root is one this
     * process is actually permitted to use: the hidden
     * `ActivityManager.forceStopPackage` (invoked reflectively, since
     * it carries no public binding) requires the signature-level
     * `FORCE_STOP_PACKAGES` permission, and `killBackgroundProcesses`
     * requires `KILL_BACKGROUND_PROCESSES`, which this agent does not
     * declare. The platform enforces both permission checks before
     * doing any work, so a `SecurityException` from either call is a
     * definitive signal that the target package's process is still
     * alive, not a hint that some other approach might still work.
     */
    fun forceStopApp(packageName: String): TapResult {
        val am = service.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager

        val reflectiveMethod = try {
            am.javaClass.getMethod("forceStopPackage", String::class.java)
        } catch (_: ReflectiveOperationException) {
            null
        }
        if (reflectiveMethod != null) {
            val thrown = try {
                reflectiveMethod.invoke(am, packageName)
                null
            } catch (e: Exception) {
                e
            }
            classifyStopAttempt(packageName, thrown)?.let {
                if (!it.success) Log.w(TAG, "not permitted to force-stop $packageName", thrown)
                return it
            }
        }

        val fallbackThrown = try {
            am.killBackgroundProcesses(packageName)
            null
        } catch (e: Exception) {
            e
        }
        classifyStopAttempt(packageName, fallbackThrown)?.let {
            if (!it.success) Log.w(TAG, "not permitted to force-stop $packageName", fallbackThrown)
            return it
        }
        Log.e(TAG, "force-stop failed for $packageName", fallbackThrown)
        return TapResult(false, "Could not stop $packageName.")
    }

    companion object {
        private const val TAG = "SystemActionDispatcher"

        // Caps only the caller-supplied value named in a validation message,
        // never the whole message, so an oversized key or package name
        // cannot push the supported-keys list out of the response.
        private const val MAX_VALIDATION_VALUE_LENGTH = 32
        private const val VALUE_TRUNCATION_MARKER = "..."

        /** Keys `pressKey` accepts, mapped to the global action each one dispatches. */
        private val KEY_ACTIONS = mapOf(
            "back" to AccessibilityService.GLOBAL_ACTION_BACK,
            "home" to AccessibilityService.GLOBAL_ACTION_HOME,
        )

        /** The key names a press_key request may use. */
        val SUPPORTED_KEYS: Set<String> get() = KEY_ACTIONS.keys

        /** Caller-facing message for a press_key request naming a key this agent does not recognize. */
        fun unsupportedKeyMessage(key: String): String =
            "Unsupported key: ${capValidationValue(key)}. Supported keys: ${SUPPORTED_KEYS.joinToString(", ")}."

        /** Caller-facing message for a launch request naming a package with no launchable activity. */
        fun noLaunchableActivityMessage(packageName: String): String =
            "No launchable activity for package: ${capValidationValue(packageName)}."

        private fun capValidationValue(value: String): String =
            if (value.length > MAX_VALIDATION_VALUE_LENGTH) {
                value.take(MAX_VALIDATION_VALUE_LENGTH) + VALUE_TRUNCATION_MARKER
            } else {
                value
            }

        /**
         * Classifies one stop attempt from what it threw. Returns a
         * success result when nothing was thrown, a failure result
         * when the platform refused the call on a permission check,
         * and null when the attempt failed for some other reason, so
         * the caller falls through to try the next available
         * strategy.
         */
        internal fun classifyStopAttempt(packageName: String, thrown: Throwable?): TapResult? {
            if (thrown == null) return TapResult(true, "stopped $packageName")
            val cause = (thrown as? InvocationTargetException)?.targetException ?: thrown
            if (cause is SecurityException) {
                return TapResult(false, "Not permitted to stop $packageName.")
            }
            return null
        }
    }
}
