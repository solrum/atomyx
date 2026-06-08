package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.os.Bundle
import android.view.accessibility.AccessibilityNodeInfo

/**
 * Clears the currently focused input field. Works even when the field has
 * no stable selector — which is the common case for Flutter apps where text
 * fields are exposed as plain views without resourceId / contentDesc. Caller
 * must focus the target first with a tap.
 *
 * Strategy chain:
 *
 *   1. ACTION_SET_TEXT(""): fast path for native EditText and Flutter inputs
 *      that proxy through Semantics. If the focused node accepts the action,
 *      done in <10ms.
 *
 *   2. Backspace via on-screen keypad: when ACTION_SET_TEXT is rejected
 *      (Flutter custom inputs without Semantics text-edit support), read the
 *      focused node's text length and tap an on-screen backspace key that
 *      many character times.
 */
class FocusedInputClearer(
    private val service: AccessibilityService,
    private val uiTree: UiTreeService,
    private val gestures: GestureDispatcher,
) {

    fun clearFocusedInput(): TapResult {
        val root = service.rootInActiveWindow
            ?: return TapResult(false, "no active window — is the device unlocked?")
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return TapResult(false, "no input-focused node — tap the target field first")
        try {
            // Strategy 1: ACTION_SET_TEXT — fast path for native EditText
            // and Flutter inputs that proxy through Semantics.
            val args = Bundle().apply {
                putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, "")
            }
            if (focused.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
                return TapResult(true, "ok via ACTION_SET_TEXT")
            }

            // Strategy 2: backspace via on-screen keypad — for Flutter custom
            // inputs that don't accept ACTION_SET_TEXT.
            val existing = focused.text?.toString() ?: ""
            if (existing.isEmpty()) return TapResult(true, "ok (already empty)")
            uiTree.markDirty()
            val summary = uiTree.dumpCompact()
            val backspace = findBackspaceKey(summary)
                ?: return TapResult(
                    false,
                    "ACTION_SET_TEXT rejected and no on-screen backspace key found " +
                        "(custom keypad must expose a delete/clear button labeled with " +
                        "⌫, del, 削除, etc.). Existing text length: ${existing.length}",
                )
            val taps = existing.length + 1
            for (i in 0 until taps) {
                gestures.tapAt(
                    backspace.bounds.left + (backspace.bounds.right - backspace.bounds.left) / 2f,
                    backspace.bounds.top + (backspace.bounds.bottom - backspace.bounds.top) / 2f,
                )
                try { Thread.sleep(80L) } catch (_: InterruptedException) {}
            }
            return TapResult(true, "ok via on-screen backspace ($taps taps)")
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }
}
