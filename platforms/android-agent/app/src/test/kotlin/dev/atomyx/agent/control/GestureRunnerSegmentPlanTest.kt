package dev.atomyx.agent.control

import dev.atomyx.agent.control.GestureRunner.PointerPath
import dev.atomyx.agent.control.GestureRunner.Waypoint
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure-function tests for `GestureRunner.planMultiPointerSegments`,
 * the per-time-segment lockstep plan that both the single-pointer and
 * the N-pointer dispatch paths build a `GestureDescription` chain
 * from. These run without an Android runtime: the planner touches no
 * `AccessibilityService` / `GestureDescription` / `Path` surface.
 */
class GestureRunnerSegmentPlanTest {

    private fun wp(phase: String, x: Float, y: Float, offset: Double) =
        Waypoint(phase, x, y, offset, pressure = null)

    @Test
    fun singlePointerMoveSegmentMatchesAuthoredDuration() {
        // A move segment must issue the full authored duration and
        // carry no post-dispatch sleep, exactly as dispatchMultiPhase
        // does for one pointer.
        val pointer = PointerPath(
            "f1",
            listOf(
                wp("down", 0f, 0f, 0.0),
                wp("up", 100f, 0f, 0.3),
            ),
        )
        val segments = GestureRunner.planMultiPointerSegments(listOf(pointer))

        assertEquals(1, segments.size)
        val seg = segments.single().pointerSegments.single()
        assertEquals(300L, seg.strokeDurationMs)
        assertFalse(seg.willContinue)
        assertEquals(0L, segments.single().postSleepMs)
    }

    @Test
    fun singlePointerHoldSegmentCapsStrokeAndSleepsRemainder() {
        // A zero-length (hold) segment must cap the issued stroke at
        // MIN_PRESS_MS and push the rest of its authored duration into
        // postSleepMs, matching dispatchMultiPhase's hold-as-sleep
        // behavior for one pointer.
        val pointer = PointerPath(
            "f1",
            listOf(
                wp("down", 50f, 50f, 0.0),
                wp("move", 50f, 50f, 1.5),
                wp("up", 200f, 50f, 2.0),
            ),
        )
        val segments = GestureRunner.planMultiPointerSegments(listOf(pointer))

        assertEquals(2, segments.size)
        val holdSegment = segments[0]
        val holdStroke = holdSegment.pointerSegments.single()
        assertEquals(GestureRunner.MIN_PRESS_MS, holdStroke.strokeDurationMs)
        assertTrue(holdStroke.willContinue)
        assertEquals(1500L - GestureRunner.MIN_PRESS_MS, holdSegment.postSleepMs)

        val moveSegment = segments[1]
        val moveStroke = moveSegment.pointerSegments.single()
        assertEquals(500L, moveStroke.strokeDurationMs)
        assertFalse(moveStroke.willContinue)
        assertEquals(0L, moveSegment.postSleepMs)
    }

    @Test
    fun twoPointersInLockstepShareEverySegment() {
        // A synchronized two-pointer gesture (pinch, rotate) must
        // produce one segment per shared waypoint boundary, each
        // segment carrying one stroke per pointer, continuing every
        // pointer until both release together on the final segment.
        val pointerA = PointerPath(
            "p1",
            listOf(
                wp("down", 0f, 0f, 0.0),
                wp("move", 10f, 0f, 0.1),
                wp("up", 20f, 0f, 0.2),
            ),
        )
        val pointerB = PointerPath(
            "p2",
            listOf(
                wp("down", 100f, 0f, 0.0),
                wp("move", 90f, 0f, 0.1),
                wp("up", 80f, 0f, 0.2),
            ),
        )
        val segments = GestureRunner.planMultiPointerSegments(listOf(pointerA, pointerB))

        assertEquals(2, segments.size)
        for (segment in segments) {
            assertEquals(2, segment.pointerSegments.size)
        }
        assertTrue(segments[0].pointerSegments.all { it.willContinue })
        assertTrue(segments[1].pointerSegments.none { it.willContinue })
    }

    @Test
    fun shorterPointerIsAbsentFromLaterSegments() {
        // A pointer that finishes (reaches its own last waypoint)
        // before the others must not appear in any later segment: the
        // platform has already released its touch.
        val shortPointer = PointerPath(
            "p1",
            listOf(wp("down", 0f, 0f, 0.0), wp("up", 10f, 0f, 0.1)),
        )
        val longPointer = PointerPath(
            "p2",
            listOf(
                wp("down", 100f, 0f, 0.0),
                wp("move", 90f, 0f, 0.1),
                wp("up", 80f, 0f, 0.2),
            ),
        )
        val segments = GestureRunner.planMultiPointerSegments(listOf(shortPointer, longPointer))

        assertEquals(2, segments.size)
        assertEquals(2, segments[0].pointerSegments.size)
        assertEquals(1, segments[1].pointerSegments.size)
        assertEquals(1, segments[1].pointerSegments.single().pointerIndex)
    }
}
