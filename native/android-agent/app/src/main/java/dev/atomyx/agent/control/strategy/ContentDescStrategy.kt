package dev.atomyx.agent.control.strategy

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.SelectorResolver

/**
 * No native indexed API for contentDescription — manual tree walk.
 * Slower but works for elements that only expose accessibility descriptions.
 */
class ContentDescStrategy : ResolutionStrategy {
    override val name = "contentDesc"

    override fun canResolve(selector: SelectorResolver.Selector): Boolean =
        !selector.contentDesc.isNullOrBlank()

    override fun resolve(
        selector: SelectorResolver.Selector,
        service: AccessibilityService,
    ): List<AccessibilityNodeInfo> {
        val desc = selector.contentDesc ?: return emptyList()
        val result = mutableListOf<AccessibilityNodeInfo>()
        for (window in safeWindows(service)) {
            val root = try { window.root } catch (_: Exception) { null } ?: continue
            try {
                walkAndCollect(root, result) { node ->
                    val nd = node.contentDescription?.toString()
                    nd == desc && isUsable(node)
                }
            } finally {
                root.recycle()
            }
        }
        return result
    }
}
