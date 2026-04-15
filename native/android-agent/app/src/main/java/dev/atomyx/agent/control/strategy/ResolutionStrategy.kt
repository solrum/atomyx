package dev.atomyx.agent.control.strategy

import android.accessibilityservice.AccessibilityService
import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.SelectorResolver

/**
 * Pluggable element resolution strategy. Each strategy handles ONE type
 * of selector criterion via the fastest path available.
 *
 * Adding a new selector type (XPath, ARIA role, etc):
 *   1. Implement ResolutionStrategy
 *   2. Add to defaultStrategies()
 *   No existing code needs to change. Closed for modification, open for extension.
 */
interface ResolutionStrategy {
    val name: String
    fun canResolve(selector: SelectorResolver.Selector): Boolean
    fun resolve(
        selector: SelectorResolver.Selector,
        service: AccessibilityService,
    ): List<AccessibilityNodeInfo>
}

// ────────────────────────────────────────────────────────────────────
// Helpers shared across strategies
// ────────────────────────────────────────────────────────────────────

internal fun safeWindows(service: AccessibilityService) =
    try { service.windows ?: emptyList() } catch (_: Exception) { emptyList() }

internal fun isUsable(node: AccessibilityNodeInfo): Boolean {
    val rect = Rect()
    node.getBoundsInScreen(rect)
    return rect.left >= 0 && rect.top >= 0 && rect.width() > 0 && rect.height() > 0
}

internal fun walkAndCollect(
    node: AccessibilityNodeInfo,
    out: MutableList<AccessibilityNodeInfo>,
    predicate: (AccessibilityNodeInfo) -> Boolean,
) {
    if (predicate(node)) {
        out.add(AccessibilityNodeInfo.obtain(node))
    }
    val count = node.childCount
    for (i in 0 until count) {
        val child = try { node.getChild(i) } catch (_: Exception) { null } ?: continue
        try {
            walkAndCollect(child, out, predicate)
        } finally {
            child.recycle()
        }
    }
}
