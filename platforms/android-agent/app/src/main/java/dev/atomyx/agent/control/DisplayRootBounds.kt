package dev.atomyx.agent.control

/**
 * Bounds of the whole display in the current rotation, anchored at the
 * origin. Returns null when either dimension is not positive, so the tree
 * root carries no bounds rather than a misleading empty rectangle.
 *
 * Hosts read these bounds as the screen size. They cannot be derived from
 * the app window because that window is absent while an activity is being
 * recreated and is offset by system bars in landscape.
 */
fun displayRootBounds(displayWidth: Int, displayHeight: Int): BoundsDto? =
    if (displayWidth > 0 && displayHeight > 0) BoundsDto(0, 0, displayWidth, displayHeight) else null
