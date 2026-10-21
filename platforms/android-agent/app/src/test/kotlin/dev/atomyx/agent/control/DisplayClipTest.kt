package dev.atomyx.agent.control

import org.junit.Assert.assertEquals
import org.junit.Test

class DisplayClipTest {

    @Test
    fun clipsABottomEdgeThatExceedsTheDisplayHeight() {
        val clipped = clipToDisplay(93, 617, 2274, 1634, 2400, 1080)

        assertEquals(BoundsDto(93, 617, 2274, 1080), clipped)
    }

    @Test
    fun clipsARightEdgeThatExceedsTheDisplayWidth() {
        val clipped = clipToDisplay(0, 0, 2500, 500, 2400, 1080)

        assertEquals(BoundsDto(0, 0, 2400, 500), clipped)
    }

    @Test
    fun clipsNegativeOrigins() {
        val clipped = clipToDisplay(-20, -5, 300, 400, 1080, 2400)

        assertEquals(BoundsDto(0, 0, 300, 400), clipped)
    }

    @Test
    fun leavesBoundsInsideTheDisplayUnchanged() {
        assertEquals(BoundsDto(10, 20, 30, 40), clipToDisplay(10, 20, 30, 40, 1080, 2400))
    }

    @Test
    fun collapsesBoundsFullyOutsideTheDisplayToAnEmptyRectangle() {
        val clipped = clipToDisplay(0, 1200, 500, 1600, 1080, 1080)

        assertEquals(0, clipped.bottom - clipped.top)
    }
}
