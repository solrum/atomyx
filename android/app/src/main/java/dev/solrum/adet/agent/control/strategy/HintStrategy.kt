package dev.solrum.adet.agent.control.strategy

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityNodeInfo
import dev.solrum.adet.agent.control.SelectorResolver

/**
 * Fuzzy multi-source matcher: substring search across text, contentDesc,
 * and resourceId. Lowest priority — try after structured strategies fail.
 */
class HintStrategy : ResolutionStrategy {
    override val name = "hint"

    override fun canResolve(selector: SelectorResolver.Selector): Boolean =
        !selector.hint.isNullOrBlank()

    override fun resolve(
        selector: SelectorResolver.Selector,
        service: AccessibilityService,
    ): List<AccessibilityNodeInfo> {
        val needle = selector.hint?.lowercase() ?: return emptyList()
        val result = mutableListOf<AccessibilityNodeInfo>()
        for (window in safeWindows(service)) {
            val root = try { window.root } catch (_: Exception) { null } ?: continue
            try {
                walkAndCollect(root, result) { node ->
                    val parts = listOfNotNull(
                        node.text?.toString(),
                        node.contentDescription?.toString(),
                        node.viewIdResourceName,
                    )
                    parts.any { it.lowercase().contains(needle) } && isUsable(node)
                }
            } finally {
                root.recycle()
            }
        }
        return result
    }
}
