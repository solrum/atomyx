package dev.atomyx.agent.control.clear

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.GestureDispatcher
import dev.atomyx.agent.control.TapResult
import dev.atomyx.agent.control.UiTreeService

/**
 * Four-strategy chain for clearing a focused input field. Tries each
 * strategy in order; falls through on failure. Caller must have already
 * focused the target field before invoking.
 *
 * Strategy order:
 *   1. SetTextStrategy — ACTION_SET_TEXT("") on the focused node.
 *   2. SelectAllKeyEventStrategy — ACTION_SET_SELECTION(0,len) + ACTION_CUT.
 *   3. LongPressSelectAllStrategy — long-press → poll for menu → tap → cut.
 *   4. KeypadBackspaceStrategy — tap an on-screen backspace key N times.
 */
class ClearTextChain(
    private val service: AccessibilityService,
    private val uiTree: UiTreeService,
    private val gestures: GestureDispatcher,
) {
    private val strategies: List<ClearTextStrategy> = listOf(
        SetTextStrategy(),
        SelectAllKeyEventStrategy(),
        LongPressSelectAllStrategy(),
        KeypadBackspaceStrategy(),
    )

    fun clear(): TapResult {
        val root = service.rootInActiveWindow
            ?: return TapResult(false, "no active window")
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return TapResult(false, "no input-focused node — tap the target field first")

        val initialText: String
        val hintText: CharSequence?
        try {
            initialText = focused.text?.toString() ?: ""
            hintText = focused.hintText
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }

        if (initialText.isEmpty() || (!hintText.isNullOrEmpty() && initialText == hintText.toString())) {
            return TapResult(true, "ok (already empty)")
        }

        val ctx = ClearContext(service, uiTree, gestures, hintText, initialText)
        val tried = mutableListOf<String>()

        for (strategy in strategies) {
            val result = strategy.attempt(ctx)
            when (result) {
                is ClearResult.Skipped -> continue
                is ClearResult.Failed -> {
                    tried.add(strategy.name)
                    if (verifyCleared(hintText)) {
                        return TapResult(true, "ok via ${strategy.name} (verify passed after failure report)")
                    }
                    continue
                }
                is ClearResult.Success -> {
                    tried.add(strategy.name)
                    if (verifyCleared(hintText)) return TapResult(true, "ok via ${strategy.name}")
                    continue
                }
            }
        }

        val lastValue = readFocusedText() ?: ""
        val focusedDesc = describeFocused() ?: "unknown"
        val diagnostic = ClearDiagnostic(tried, lastValue, focusedDesc, 0, 0)
        return TapResult(false, buildDiagnosticMessage(diagnostic))
    }

    fun clearBestEffort(@Suppress("UNUSED_PARAMETER") perKeyDelayMs: Long) {
        clear()
    }

    private fun verifyCleared(hintText: CharSequence?): Boolean {
        val root = service.rootInActiveWindow ?: return false
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return true // node gone — field cleared or dismissed
        return try {
            val text = focused.text?.toString() ?: ""
            text.isEmpty() || (!hintText.isNullOrEmpty() && text.contentEquals(hintText))
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }

    private fun readFocusedText(): String? {
        val root = service.rootInActiveWindow ?: return null
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT) ?: return null
        return try {
            focused.text?.toString()
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }

    private fun describeFocused(): String? {
        val root = service.rootInActiveWindow ?: return null
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT) ?: return null
        return try {
            "${focused.className}(text=${focused.text}, hint=${focused.hintText})"
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }

    private fun buildDiagnosticMessage(d: ClearDiagnostic): String =
        "clear_failed: all ${d.strategiesTried.size} strategies failed. " +
        "tried=[${d.strategiesTried.joinToString(",")}] " +
        "lastValue=\"${d.lastValue}\" focusedNode=${d.focusedNodeDesc}"
}
