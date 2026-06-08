package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService

/**
 * Dismisses the on-screen keyboard without triggering navigation.
 * Uses GLOBAL_ACTION_BACK only when a keyboard is visible — safe to call
 * anytime; a no-op when no keyboard is showing.
 */
class KeyboardHider(
    private val service: AccessibilityService,
    private val uiTree: UiTreeService,
) {

    fun hideKeyboard(): TapResult {
        uiTree.markDirty()
        val kb = uiTree.getKeyboardInfo()
        if (!kb.visible) return TapResult(true, "keyboard not visible — no-op")
        service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_BACK)
        // Poll until the IME window disappears instead of a fixed sleep.
        // Exponential backoff (50/100/200 ms, ~1s cap) returns quickly on
        // fast dismiss animations and tolerates slower devices without
        // inflating the common-case latency.
        val deadline = System.currentTimeMillis() + 1000L
        var wait = 50L
        while (System.currentTimeMillis() < deadline) {
            try { Thread.sleep(wait) } catch (_: InterruptedException) {}
            uiTree.markDirty()
            val after = uiTree.getKeyboardInfo()
            if (!after.visible) return TapResult(true, "keyboard dismissed")
            wait = minOf(wait * 2, 200L)
        }
        return TapResult(true, "keyboard dismiss dispatched (poll timeout — IME may still animate)")
    }
}
