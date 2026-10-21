package dev.atomyx.agent.control

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class DisplayRootBoundsTest {

    @Test
    fun anchorsPortraitBoundsAtTheOrigin() {
        assertEquals(BoundsDto(0, 0, 1080, 2400), displayRootBounds(1080, 2400))
    }

    @Test
    fun anchorsLandscapeBoundsAtTheOrigin() {
        assertEquals(BoundsDto(0, 0, 2400, 1080), displayRootBounds(2400, 1080))
    }

    @Test
    fun omitsBoundsWhenADimensionIsNotPositive() {
        assertNull(displayRootBounds(0, 2400))
        assertNull(displayRootBounds(1080, 0))
        assertNull(displayRootBounds(-1, -1))
    }
}
