package dev.solrum.adet.agent.control.strategy

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityNodeInfo
import dev.solrum.adet.agent.control.SelectorResolver

/**
 * Fast path: native findAccessibilityNodeInfosByText. Filters to exact match
 * since the native API is partial-match.
 */
class TextStrategy : ResolutionStrategy {
    override val name = "text"

    override fun canResolve(selector: SelectorResolver.Selector): Boolean =
        !selector.text.isNullOrBlank()

    override fun resolve(
        selector: SelectorResolver.Selector,
        service: AccessibilityService,
    ): List<AccessibilityNodeInfo> {
        val text = selector.text ?: return emptyList()
        val result = mutableListOf<AccessibilityNodeInfo>()
        for (window in safeWindows(service)) {
            val root = try { window.root } catch (_: Exception) { null } ?: continue
            try {
                val matches = root.findAccessibilityNodeInfosByText(text) ?: emptyList()
                for (m in matches) {
                    val mt = m.text?.toString()
                    if (mt == text && isUsable(m)) result.add(m) else m.recycle()
                }
            } finally {
                root.recycle()
            }
        }
        return result
    }
}

class TextContainsStrategy : ResolutionStrategy {
    override val name = "textContains"

    override fun canResolve(selector: SelectorResolver.Selector): Boolean =
        !selector.textContains.isNullOrBlank()

    override fun resolve(
        selector: SelectorResolver.Selector,
        service: AccessibilityService,
    ): List<AccessibilityNodeInfo> {
        val needle = selector.textContains ?: return emptyList()
        val result = mutableListOf<AccessibilityNodeInfo>()
        for (window in safeWindows(service)) {
            val root = try { window.root } catch (_: Exception) { null } ?: continue
            try {
                val matches = root.findAccessibilityNodeInfosByText(needle) ?: emptyList()
                for (m in matches) {
                    if (isUsable(m)) result.add(m) else m.recycle()
                }
            } finally {
                root.recycle()
            }
        }
        return result
    }
}
