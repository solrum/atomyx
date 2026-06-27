package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.app.ActivityManager
import android.content.Context
import android.content.Intent

/**
 * Global / OS-scope actions that don't touch the gesture pipeline:
 * navigation keys (back / home), app launch, force-stop.
 *
 * Kept separate from [GestureDispatcher] and the keyboard/input
 * classes because these paths route through `performGlobalAction`
 * or `Context` directly — they never build a `GestureDescription`
 * or read the focused-node tree.
 */
class SystemActionDispatcher(
    private val service: AccessibilityService,
) {

    fun pressKey(key: String) {
        when (key) {
            "back" -> service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_BACK)
            "home" -> service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_HOME)
            "enter" -> error("press_key('enter') not supported on non-rooted devices")
            else -> error("unknown key: $key")
        }
    }

    fun launchApp(packageName: String) {
        val intent = service.packageManager.getLaunchIntentForPackage(packageName)
            ?: error("no launch intent for $packageName")
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        service.startActivity(intent)
    }

    /**
     * Force-stop an app package. Equivalent to `adb shell am force-stop <pkg>`.
     *
     * `ActivityManager.killBackgroundProcesses` only kills background
     * processes; we reflectively invoke the hidden `forceStopPackage`
     * which is the closest equivalent available from within an app
     * process. If the reflection fails (system policy enforcement),
     * fall back to killing background processes — handles most
     * stale-state scenarios without requiring root or a signature
     * permission.
     */
    fun forceStopApp(packageName: String) {
        val am = service.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
        try {
            val m = am.javaClass.getMethod("forceStopPackage", String::class.java)
            m.invoke(am, packageName)
        } catch (_: Exception) {
            am.killBackgroundProcesses(packageName)
        }
    }
}
