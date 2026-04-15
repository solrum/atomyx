package dev.solrum.adet.agent.service

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.app.usage.UsageEvents
import android.app.usage.UsageStatsManager
import android.content.Context
import android.graphics.Bitmap
import android.os.Build
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import java.io.ByteArrayOutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Slim AccessibilityService for adet. ONLY responsibilities:
 *   1. Track foreground activity (for currentForegroundActivity helper)
 *   2. Take screenshots (API 30+)
 *   3. Fire `onTreeMaybeChanged` hook so adet's UiTreeService cache invalidates
 *
 * NO recording, NO touch interaction, NO coordinate streaming, NO upload.
 * Recording is handled by SynapseAgent (separate app) — adet is read-write
 * exploration only.
 */
class AdetAccessibilityService : AccessibilityService() {

    @Volatile private var latestForegroundPackage: String = ""
    @Volatile private var latestForegroundActivity: String = ""

    /**
     * Hook installed by AdetServices — invoked whenever any window
     * state/content/visibility event fires, telling the UiTreeService
     * cache to mark itself dirty.
     */
    var onTreeMaybeChanged: (() -> Unit)? = null

    companion object {
        private const val TAG = "AdetA11y"
        @Volatile
        var instance: AdetAccessibilityService? = null
            private set
    }

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this

        // AUGMENT — never REPLACE — the existing serviceInfo. Replacing it
        // with `AccessibilityServiceInfo()` clobbers values from accessibility_config.xml
        // (most notably canPerformGestures + canTakeScreenshot capabilities)
        // and causes service.windows to return empty until the service is re-bound.
        try {
            val current = serviceInfo ?: AccessibilityServiceInfo()
            current.flags = current.flags or
                AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS or
                AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS or
                AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS
            serviceInfo = current
            Log.i(TAG, "AdetAccessibilityService connected (capabilities=${current.capabilities}, flags=${current.flags})")
        } catch (e: Exception) {
            Log.e(TAG, "Failed to configure serviceInfo", e)
        }
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        if (event == null) return

        if (event.eventType == AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED) {
            val pkg = event.packageName?.toString() ?: ""
            val cls = event.className?.toString() ?: ""
            if (pkg.isNotEmpty()) {
                latestForegroundPackage = pkg
                latestForegroundActivity = cls
            }
            onTreeMaybeChanged?.invoke()
        } else if (event.eventType == AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED ||
                   event.eventType == AccessibilityEvent.TYPE_WINDOWS_CHANGED) {
            onTreeMaybeChanged?.invoke()
        }
    }

    override fun onInterrupt() {
        Log.w(TAG, "Service interrupted")
    }

    override fun onDestroy() {
        instance = null
        super.onDestroy()
    }

    /**
     * Capture screenshot of the current screen as PNG bytes.
     * Requires API 30+ (AccessibilityService.takeScreenshot) and
     * canTakeScreenshot=true in accessibility config.
     */
    fun takeScreenshotPng(): ByteArray? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            Log.w(TAG, "takeScreenshot requires API 30+")
            return null
        }
        val latch = CountDownLatch(1)
        var result: ByteArray? = null
        try {
            takeScreenshot(
                android.view.Display.DEFAULT_DISPLAY,
                mainExecutor,
                object : TakeScreenshotCallback {
                    override fun onSuccess(screenshot: ScreenshotResult) {
                        try {
                            val bitmap = Bitmap.wrapHardwareBuffer(
                                screenshot.hardwareBuffer,
                                screenshot.colorSpace,
                            )
                            if (bitmap != null) {
                                val baos = ByteArrayOutputStream()
                                bitmap.compress(Bitmap.CompressFormat.PNG, 100, baos)
                                result = baos.toByteArray()
                                bitmap.recycle()
                            }
                        } finally {
                            screenshot.hardwareBuffer.close()
                            latch.countDown()
                        }
                    }
                    override fun onFailure(errorCode: Int) {
                        Log.w(TAG, "takeScreenshot failed: $errorCode")
                        latch.countDown()
                    }
                },
            )
            latch.await(2, TimeUnit.SECONDS)
        } catch (e: Exception) {
            Log.e(TAG, "takeScreenshotPng error", e)
        }
        return result
    }

    /**
     * Best-effort foreground activity lookup. Primary: latest TYPE_WINDOW_STATE_CHANGED.
     * Fallback: UsageStatsManager (requires PACKAGE_USAGE_STATS).
     */
    fun currentForegroundActivity(): Map<String, String> {
        if (latestForegroundPackage.isNotEmpty()) {
            return mapOf(
                "packageName" to latestForegroundPackage,
                "activity" to latestForegroundActivity,
                "source" to "accessibility",
            )
        }
        return try {
            val usm = getSystemService(Context.USAGE_STATS_SERVICE) as UsageStatsManager
            val now = System.currentTimeMillis()
            val events = usm.queryEvents(now - 60_000, now)
            var pkg = ""
            var cls = ""
            val ev = UsageEvents.Event()
            while (events.hasNextEvent()) {
                events.getNextEvent(ev)
                if (ev.eventType == UsageEvents.Event.MOVE_TO_FOREGROUND) {
                    pkg = ev.packageName ?: ""
                    cls = ev.className ?: ""
                }
            }
            mapOf("packageName" to pkg, "activity" to cls, "source" to "usage_stats")
        } catch (e: Exception) {
            Log.w(TAG, "currentForegroundActivity error", e)
            mapOf("packageName" to "", "activity" to "", "source" to "none")
        }
    }
}
