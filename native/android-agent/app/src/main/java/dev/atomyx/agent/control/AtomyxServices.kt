package dev.atomyx.agent.control

import dev.atomyx.agent.service.AtomyxAccessibilityService

/**
 * DI container for the Atomyx control plane. Holds all collaborators with
 * lifecycle tied to the AccessibilityService instance. Routes receive an
 * AtomyxServices reference instead of constructing dependencies themselves.
 *
 * Resolved lazily per accessibility instance — when the user toggles
 * accessibility off/on, a new instance is created.
 */
class AtomyxServices(val accessibility: AtomyxAccessibilityService) {

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
 * Lazy holder that creates a fresh AtomyxServices each time the underlying
 * accessibility service instance changes. Used by HttpControlServer.
 */
class AtomyxServicesHolder(private val accessibilityProvider: () -> AtomyxAccessibilityService?) {

    private var cached: AtomyxServices? = null
    private var cachedFor: AtomyxAccessibilityService? = null

    @Synchronized
    fun get(): AtomyxServices? {
        val current = accessibilityProvider() ?: return null
        if (cachedFor !== current) {
            cached?.shutdown()
            cached = AtomyxServices(current)
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
