package dev.atomyx.agent.control.clear

import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.CompactElement

class LongPressSelectAllStrategy : ClearTextStrategy {
    override val name = "long_press_select_all"

    companion object {
        private const val LONG_PRESS_DURATION_MS = 700L
        private const val SELECT_ALL_POLL_BUDGET_MS = 2000L
        private const val SELECT_ALL_POLL_INTERVAL_MS = 100L
        private const val POST_TAP_SETTLE_MS = 150L
    }

    private val selectAllLabels = listOf(
        "select all",
        "chọn tất cả",
        "全选",
        "すべて選択",
        "모두 선택",
        "seleccionar todo",
        "tout sélectionner",
        "selecionar tudo",
        "alles auswählen",
        "выбрать всё",
    )

    override fun attempt(context: ClearContext): ClearResult {
        val root = context.service.rootInActiveWindow
            ?: return ClearResult.Failed("no active window")
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return ClearResult.Failed("no focused input node")

        val cx: Float
        val cy: Float
        try {
            val bounds = Rect()
            focused.getBoundsInScreen(bounds)
            cx = (bounds.left + bounds.right) / 2f
            cy = (bounds.top + bounds.bottom) / 2f
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }

        context.gestures.longPressAt(cx, cy, LONG_PRESS_DURATION_MS)

        val deadline = System.currentTimeMillis() + SELECT_ALL_POLL_BUDGET_MS
        var selectAllEl: CompactElement? = null
        while (System.currentTimeMillis() < deadline) {
            context.uiTree.markDirty()
            val summary = context.uiTree.dumpCompact()
            val found = findSelectAllItem(summary)
            if (found != null) {
                selectAllEl = found
                break
            }
            try { Thread.sleep(SELECT_ALL_POLL_INTERVAL_MS) } catch (_: InterruptedException) {}
        }

        if (selectAllEl == null) {
            return ClearResult.Failed(
                "long-press did not surface a 'select all' menu item within ${SELECT_ALL_POLL_BUDGET_MS}ms",
            )
        }

        context.gestures.tapAt(
            selectAllEl.bounds.left + (selectAllEl.bounds.right - selectAllEl.bounds.left) / 2f,
            selectAllEl.bounds.top + (selectAllEl.bounds.bottom - selectAllEl.bounds.top) / 2f,
        )

        try { Thread.sleep(POST_TAP_SETTLE_MS) } catch (_: InterruptedException) {}

        val root2 = context.service.rootInActiveWindow
            ?: return ClearResult.Failed("no active window after select-all tap")
        val focused2 = root2.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return ClearResult.Failed("focused input lost after select-all tap")
        return try {
            if (focused2.performAction(AccessibilityNodeInfo.ACTION_CUT)) {
                ClearResult.Success
            } else {
                ClearResult.Failed("ACTION_CUT rejected after select-all")
            }
        } finally {
            try { focused2.recycle() } catch (_: Exception) {}
        }
    }

    private fun findSelectAllItem(summary: List<CompactElement>): CompactElement? {
        for (el in summary) {
            if (!el.enabled || !el.clickable) continue
            val label = el.label.trim()
            if (selectAllLabels.any { label.equals(it, ignoreCase = true) }) return el
        }
        return null
    }
}
