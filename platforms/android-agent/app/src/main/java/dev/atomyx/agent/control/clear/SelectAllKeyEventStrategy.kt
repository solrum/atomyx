package dev.atomyx.agent.control.clear

import android.os.Bundle
import android.view.accessibility.AccessibilityNodeInfo

class SelectAllKeyEventStrategy : ClearTextStrategy {
    override val name = "select_all_cut"

    override fun attempt(context: ClearContext): ClearResult {
        val root = context.service.rootInActiveWindow
            ?: return ClearResult.Failed("no active window")
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return ClearResult.Failed("no focused input node")
        return try {
            val textLength = focused.text?.length ?: 0
            if (textLength == 0) return ClearResult.Success
            val selBundle = Bundle().apply {
                putInt(AccessibilityNodeInfo.ACTION_ARGUMENT_SELECTION_START_INT, 0)
                putInt(AccessibilityNodeInfo.ACTION_ARGUMENT_SELECTION_END_INT, textLength)
            }
            val selected = focused.performAction(
                AccessibilityNodeInfo.ACTION_SET_SELECTION, selBundle,
            )
            if (!selected) return ClearResult.Failed("ACTION_SET_SELECTION rejected")
            val cut = focused.performAction(AccessibilityNodeInfo.ACTION_CUT)
            if (cut) ClearResult.Success else ClearResult.Failed("ACTION_CUT rejected")
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }
}
