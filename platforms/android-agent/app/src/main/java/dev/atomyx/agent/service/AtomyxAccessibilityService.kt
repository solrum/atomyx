package dev.atomyx.agent.service

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.app.usage.UsageEvents
import android.app.usage.UsageStatsManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.os.Build
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityManager
import java.io.ByteArrayOutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Slim AccessibilityService for Atomyx. ONLY responsibilities:
 *   1. Track foreground activity (for currentForegroundActivity helper)
 *   2. Take screenshots (API 30+)
 *   3. Fire `onTreeMaybeChanged` hook so Atomyx's UiTreeService cache invalidates
 *
 * NO recording, NO touch interaction, NO coordinate streaming, NO upload.
 * Recording is out of scope for this agent, because Atomyx is read-write
 * exploration only.
 */
class AtomyxAccessibilityService : AccessibilityService() {

    @Volatile private var latestForegroundPackage: String = ""
    @Volatile private var latestForegroundActivity: String = ""

    /**
     * Hook installed by AtomyxServices. Invoked whenever any window
     * state/content/visibility event fires, telling the UiTreeService
     * cache to mark itself dirty.
     */
    var onTreeMaybeChanged: (() -> Unit)? = null

    companion object {
        private const val TAG = "AtomyxA11y"

        /** Max width or height for screenshots sent to the MCP channel. */
        private const val MAX_SCREENSHOT_DIM = 540
        /** JPEG quality (0–100). 80 is a good trade-off: ~5–10× smaller than PNG. */
        private const val SCREENSHOT_QUALITY = 80

        /**
         * Runs `takeScreenshot`'s result callback and JPEG encode off
         * the main thread, so a mid-gesture capture's encode never
         * contends with the main-thread Looper that also delivers
         * `AccessibilityService.dispatchGesture`'s completion
         * callbacks.
         */
        private val screenshotExecutor: Executor = Executors.newSingleThreadExecutor { r ->
            Thread(r, "atomyx-screenshot-encode").apply { isDaemon = true }
        }

        @Volatile
        var instance: AtomyxAccessibilityService? = null
            private set

        /**
         * Down-scale a bitmap so its longest side is at most [maxDim] px.
         * Returns the original bitmap unchanged if it already fits.
         */
        private fun scaleBitmap(src: Bitmap, maxDim: Int): Bitmap {
            val w = src.width
            val h = src.height
            if (w <= maxDim && h <= maxDim) return src
            val scale = maxDim.toFloat() / maxOf(w, h)
            val newW = (w * scale).toInt()
            val newH = (h * scale).toInt()
            return Bitmap.createScaledBitmap(src, newW, newH, true)
        }
    }

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this

        // AUGMENT the existing serviceInfo. Never REPLACE it. Replacing it
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
            Log.i(TAG, "AtomyxAccessibilityService connected (capabilities=${current.capabilities}, flags=${current.flags})")
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
        // The system uses this to pause event delivery temporarily, not to
        // tear down the binding, so the instance stays published. /health's
        // canReadScreen() probes the live service on every call regardless,
        // so a service left unusable after an interrupt is still caught
        // without needing a flag set here.
        Log.w(TAG, "Service interrupted")
    }

    override fun onUnbind(intent: Intent?): Boolean {
        instance = null
        return super.onUnbind(intent)
    }

    override fun onDestroy() {
        instance = null
        super.onDestroy()
    }

    /**
     * Proves the service can currently produce a screen, not merely that its
     * binder connection exists. The system can leave a service instance
     * published while the underlying binding has gone bad without ever
     * calling onUnbind or onDestroy, so /health must check this live rather
     * than trust the published instance alone.
     */
    fun canReadScreen(): Boolean = try {
        !windows.isNullOrEmpty()
    } catch (e: Exception) {
        Log.w(TAG, "canReadScreen error", e)
        false
    }

    /**
     * Confirms this component is still in the platform's own list of
     * running accessibility services, catching a dropped binding that
     * left the published instance in place.
     */
    fun isRegisteredWithSystem(): Boolean = try {
        val manager = getSystemService(Context.ACCESSIBILITY_SERVICE) as AccessibilityManager
        val expected = ComponentName(this, AtomyxAccessibilityService::class.java)
        manager.getEnabledAccessibilityServiceList(AccessibilityServiceInfo.FEEDBACK_ALL_MASK)
            .any { ComponentName.unflattenFromString(it.id) == expected }
    } catch (e: Exception) {
        Log.w(TAG, "isRegisteredWithSystem error", e)
        false
    }

    /**
     * Capture screenshot of the current screen as JPEG bytes.
     * Requires API 30+ (AccessibilityService.takeScreenshot) and
     * canTakeScreenshot=true in accessibility config.
     */
    fun takeScreenshotJpeg(): ByteArray? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            Log.w(TAG, "takeScreenshot requires API 30+")
            return null
        }
        val latch = CountDownLatch(1)
        var result: ByteArray? = null
        try {
            takeScreenshot(
                android.view.Display.DEFAULT_DISPLAY,
                screenshotExecutor,
                object : TakeScreenshotCallback {
                    override fun onSuccess(screenshot: ScreenshotResult) {
                        try {
                            val bitmap = Bitmap.wrapHardwareBuffer(
                                screenshot.hardwareBuffer,
                                screenshot.colorSpace,
                            )
                            if (bitmap != null) {
                                val scaled = scaleBitmap(bitmap, MAX_SCREENSHOT_DIM)
                                val baos = ByteArrayOutputStream()
                                scaled.compress(Bitmap.CompressFormat.JPEG, SCREENSHOT_QUALITY, baos)
                                result = baos.toByteArray()
                                if (scaled !== bitmap) scaled.recycle()
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
            Log.e(TAG, "takeScreenshotJpeg error", e)
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
