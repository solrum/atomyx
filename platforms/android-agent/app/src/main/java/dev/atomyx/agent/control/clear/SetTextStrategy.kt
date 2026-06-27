package dev.atomyx.agent.control.clear

import android.os.Bundle
import android.view.accessibility.AccessibilityNodeInfo

class SetTextStrategy : ClearTextStrategy {
    override val name = "set_text"

    override fun attempt(context: ClearContext): ClearResult {
        if (!context.hintText.isNullOrEmpty() && context.initialText == context.hintText.toString()) {
            return ClearResult.Skipped
        }
        val root = context.service.rootInActiveWindow
            ?: return ClearResult.Failed("no active window")
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return ClearResult.Failed("no focused input node")
        return try {
            val args = Bundle().apply {
                putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, "")
            }
            if (focused.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
                ClearResult.Success
            } else {
                ClearResult.Failed("ACTION_SET_TEXT rejected")
            }
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }
}
