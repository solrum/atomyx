package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.strategy.ContentDescStrategy
import dev.atomyx.agent.control.strategy.HintStrategy
import dev.atomyx.agent.control.strategy.ResolutionStrategy
import dev.atomyx.agent.control.strategy.ResourceIdStrategy
import dev.atomyx.agent.control.strategy.TextContainsStrategy
import dev.atomyx.agent.control.strategy.TextStrategy

/**
 * Resolves a selector to a live AccessibilityNodeInfo via a chain of
 * pluggable strategies. The first matching strategy wins. Strategies are
 * tried in priority order (fastest first).
 *
 * Adding a new selector type:
 *   1. Implement ResolutionStrategy
 *   2. Add to the strategies list (or pass via constructor)
 *   3. Add the field to Selector data class
 *   No existing strategies need to change.
 *
 * Caller is responsible for recycling returned nodes.
 */
class SelectorResolver(
    private val service: AccessibilityService,
    private val strategies: List<ResolutionStrategy> = defaultStrategies(),
) {

    data class Selector(
        val resourceId: String? = null,
        val contentDesc: String? = null,
        val text: String? = null,
        val textContains: String? = null,
        val hint: String? = null,
        val nth: Int = 0,
    )

    data class Resolved(
        val node: AccessibilityNodeInfo,
        val bounds: Rect,
        val resolvedBy: String,
    )

    fun resolve(selector: Selector): Resolved? {
        for (strategy in strategies) {
            if (!strategy.canResolve(selector)) continue
            val candidates = strategy.resolve(selector, service)
            if (candidates.isEmpty()) continue

            val picked = candidates.getOrNull(selector.nth) ?: candidates.first()
            val bounds = Rect()
            picked.getBoundsInScreen(bounds)

            // Recycle non-picked candidates
            for (c in candidates) {
                if (c !== picked) {
                    try { c.recycle() } catch (_: Exception) {}
                }
            }

            return Resolved(picked, bounds, strategy.name)
        }
        return null
    }

    companion object {
        /**
         * Default strategy chain. Order matters: fastest + most specific first.
         */
        fun defaultStrategies(): List<ResolutionStrategy> = listOf(
            ResourceIdStrategy(),
            TextStrategy(),
            ContentDescStrategy(),
            TextContainsStrategy(),
            HintStrategy(),
        )
    }
}
