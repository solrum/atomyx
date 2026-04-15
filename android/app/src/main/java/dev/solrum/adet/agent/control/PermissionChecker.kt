package dev.solrum.adet.agent.control

import android.app.AppOpsManager
import android.content.Context
import android.content.Intent
import android.os.Process
import android.provider.Settings
import android.text.TextUtils
import android.view.accessibility.AccessibilityManager

/**
 * Centralized permission state for adet. All checks are pure (no side effects).
 * Use the deepLink* functions to navigate the user to the appropriate Settings page.
 */
object PermissionChecker {

    fun isAccessibilityEnabled(context: Context): Boolean {
        val expected = "${context.packageName}/dev.solrum.adet.agent.service.AdetAccessibilityService"
        return try {
            val enabled = Settings.Secure.getString(
                context.contentResolver,
                Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES,
            ) ?: return false
            val splitter = TextUtils.SimpleStringSplitter(':')
            splitter.setString(enabled)
            splitter.any { it.equals(expected, ignoreCase = true) }
        } catch (_: Exception) {
            false
        }
    }

    fun isUsageStatsGranted(context: Context): Boolean {
        return try {
            val ops = context.getSystemService(Context.APP_OPS_SERVICE) as AppOpsManager
            val mode = ops.unsafeCheckOpNoThrow(
                AppOpsManager.OPSTR_GET_USAGE_STATS,
                Process.myUid(),
                context.packageName,
            )
            mode == AppOpsManager.MODE_ALLOWED
        } catch (_: Exception) {
            false
        }
    }

    fun isAccessibilityServiceConnected(): Boolean =
        dev.solrum.adet.agent.service.AdetAccessibilityService.instance != null

    fun deepLinkAccessibility(context: Context) {
        context.startActivity(
            Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        )
    }

    fun deepLinkUsageStats(context: Context) {
        context.startActivity(
            Intent(Settings.ACTION_USAGE_ACCESS_SETTINGS)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        )
    }

    data class Status(
        val accessibilityEnabled: Boolean,
        val accessibilityConnected: Boolean,
        val usageStatsGranted: Boolean,
    ) {
        val allReady: Boolean get() = accessibilityEnabled && accessibilityConnected && usageStatsGranted
    }

    fun status(context: Context): Status = Status(
        accessibilityEnabled = isAccessibilityEnabled(context),
        accessibilityConnected = isAccessibilityServiceConnected(),
        usageStatsGranted = isUsageStatsGranted(context),
    )
}
