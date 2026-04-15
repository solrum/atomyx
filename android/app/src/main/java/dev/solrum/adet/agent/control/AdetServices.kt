package dev.solrum.adet.agent.control

import dev.solrum.adet.agent.service.AdetAccessibilityService

/**
 * DI container for the adet control plane. Holds all collaborators with
 * lifecycle tied to the AccessibilityService instance. Routes receive an
 * AdetServices reference instead of constructing dependencies themselves.
 *
 * Resolved lazily per accessibility instance — when the user toggles
 * accessibility off/on, a new instance is created.
 */
class AdetServices(val accessibility: AdetAccessibilityService) {

    val uiTree: UiTreeService = UiTreeService(accessibility)
    val resolver: SelectorResolver = SelectorResolver(accessibility)
    val gestures: GestureDispatcher = GestureDispatcher(accessibility, uiTree, resolver)

    init {
        // Cache invalidation hook — fires when the accessibility service sees
        // a window state/content/visibility change, forces next dump to rebuild.
        accessibility.onTreeMaybeChanged = { uiTree.markDirty() }
    }

    fun shutdown() {
        try { uiTree.recycleRegistry() } catch (_: Exception) {}
        accessibility.onTreeMaybeChanged = null
    }
}

/**
 * Lazy holder that creates a fresh AdetServices each time the underlying
 * accessibility service instance changes. Used by HttpControlServer.
 */
class AdetServicesHolder(private val accessibilityProvider: () -> AdetAccessibilityService?) {

    private var cached: AdetServices? = null
    private var cachedFor: AdetAccessibilityService? = null

    @Synchronized
    fun get(): AdetServices? {
        val current = accessibilityProvider() ?: return null
        if (cachedFor !== current) {
            cached?.shutdown()
            cached = AdetServices(current)
            cachedFor = current
        }
        return cached
    }

    @Synchronized
    fun shutdown() {
        cached?.shutdown()
        cached = null
        cachedFor = null
    }
}
