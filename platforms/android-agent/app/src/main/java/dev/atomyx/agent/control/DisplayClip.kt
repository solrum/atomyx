package dev.atomyx.agent.control

/**
 * Intersects the given rectangle with the display rectangle that starts at the
 * origin. A rectangle that lies fully outside the display collapses to an
 * empty one, so callers can reject it with a width or height check.
 *
 * The input method window reports bounds from the window manager that can
 * extend past the physical screen, for example in landscape, so its bounds
 * must be clipped before a host compares them with the screen size.
 */
fun clipToDisplay(left: Int, top: Int, right: Int, bottom: Int, displayWidth: Int, displayHeight: Int): BoundsDto {
    val clippedLeft = maxOf(left, 0)
    val clippedTop = maxOf(top, 0)
    val clippedRight = maxOf(minOf(right, displayWidth), clippedLeft)
    val clippedBottom = maxOf(minOf(bottom, displayHeight), clippedTop)
    return BoundsDto(clippedLeft, clippedTop, clippedRight, clippedBottom)
}
