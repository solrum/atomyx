package dev.atomyx.agent.control.strategy

import android.accessibilityservice.AccessibilityService
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.SelectorResolver

/**
 * Resolves by resourceId. Fast path uses Android's native indexed lookup
 * `findAccessibilityNodeInfosByViewId` — but that ONLY accepts fully
 * qualified `package:id/name` form. Flutter / Compose / RN apps expose
 * non-qualified ids (e.g. `G01-05-01/2`), so we fall back to a tree walk
 * matching `node.viewIdResourceName` exactly OR by suffix.
 *
 * Typical latency: 5-15ms native, 30-80ms walk fallback.
 */
class ResourceIdStrategy : ResolutionStrategy {
    override val name = "resourceId"

    override fun canResolve(selector: SelectorResolver.Selector): Boolean =
        !selector.resourceId.isNullOrBlank()

    override fun resolve(
        selector: SelectorResolver.Selector,
        service: AccessibilityService,
    ): List<AccessibilityNodeInfo> {
        val rid = selector.resourceId ?: return emptyList()
        val result = mutableListOf<AccessibilityNodeInfo>()
        for (window in safeWindows(service)) {
            val root = try { window.root } catch (_: Exception) { null } ?: continue
            try {
                // Fast path: native indexed lookup.
                val matches = root.findAccessibilityNodeInfosByViewId(rid) ?: emptyList()
                for (m in matches) {
                    if (isUsable(m)) result.add(m) else m.recycle()
                }
                // Fallback: walk the tree if native lookup found nothing.
                // Catches Flutter / Compose / RN ids that aren't fully
                // qualified with `package:id/`.
                if (result.isEmpty()) {
                    walkAndCollect(root, result) { node ->
                        val nid = try { node.viewIdResourceName } catch (_: Exception) { null }
                        if (nid.isNullOrBlank()) false
                        else nid == rid || nid.endsWith("/$rid") || nid.endsWith(rid)
                    }
                    // Drop unusable nodes captured by walk.
                    val filtered = result.filter { isUsable(it) }
                    result.filter { !filtered.contains(it) }.forEach { it.recycle() }
                    result.clear()
                    result.addAll(filtered)
                }
            } finally {
                root.recycle()
            }
        }
        return result
    }
}
