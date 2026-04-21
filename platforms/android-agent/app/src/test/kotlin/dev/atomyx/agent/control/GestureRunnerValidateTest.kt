package dev.atomyx.agent.control

import dev.atomyx.agent.control.GestureRunner.GestureCapabilities
import dev.atomyx.agent.control.GestureRunner.PointerPath
import dev.atomyx.agent.control.GestureRunner.Waypoint
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure-function tests for `GestureRunner.validatePointers`. Each
 * case pins one rejection path so a regression in the validator
 * surfaces as a named failure rather than a silent dispatch-time
 * crash on the AccessibilityService.
 *
 * These tests run without an Android runtime: `validatePointers`
 * is extracted as a companion function that touches no
 * `AccessibilityService` / `GestureDescription` surface.
 */
class GestureRunnerValidateTest {

    private val singlePointerCaps = GestureCapabilities(
        canMultiPointer = false,
        canPressure = false,
    )
    private val multiPointerCaps = GestureCapabilities(
        canMultiPointer = true,
        canPressure = false,
    )
    private val pressureCaps = GestureCapabilities(
        canMultiPointer = false,
        canPressure = true,
    )

    private fun wp(
        phase: String,
        x: Float = 100f,
        y: Float = 200f,
        offset: Double = 0.0,
        pressure: Double? = null,
    ) = Waypoint(phase, x, y, offset, pressure)

    private fun tapPath(id: String = "f1") = PointerPath(
        id,
        listOf(wp("down", offset = 0.0), wp("up", offset = 0.05)),
    )

    // ── happy paths ──────────────────────────────────────────────

    @Test
    fun singlePointerTapAccepted() {
        val result = GestureRunner.validatePointers(listOf(tapPath()), singlePointerCaps)
        assertNull("valid tap must pass validation, got: $result", result)
    }

    @Test
    fun multiPointerAcceptedWhenCapabilityTrue() {
        val result = GestureRunner.validatePointers(
            listOf(tapPath("f1"), tapPath("f2")),
            multiPointerCaps,
        )
        assertNull(result)
    }

    @Test
    fun pressureAcceptedWhenCapabilityTrue() {
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(
                    wp("down", offset = 0.0, pressure = 0.5),
                    wp("up", offset = 0.05, pressure = 0.5),
                ),
            ),
        )
        assertNull(GestureRunner.validatePointers(pointers, pressureCaps))
    }

    @Test
    fun dragWithIntermediateMovesAccepted() {
        // Validator accepts any number of intermediate `move` phases
        // between the opening `down` and closing `up`. Interpolation
        // along multi-waypoint paths is how long drags (reorder,
        // pan, dragAndDrop) express their motion.
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(
                    wp("down", 100f, 200f, 0.0),
                    wp("move", 150f, 200f, 0.1),
                    wp("move", 200f, 200f, 0.2),
                    wp("move", 250f, 200f, 0.3),
                    wp("up", 300f, 200f, 0.4),
                ),
            ),
        )
        assertNull(GestureRunner.validatePointers(pointers, singlePointerCaps))
    }

    // ── rejection paths ──────────────────────────────────────────

    @Test
    fun emptyPointersRejected() {
        val result = GestureRunner.validatePointers(emptyList(), singlePointerCaps)
        assertEquals("empty pointers array", result)
    }

    @Test
    fun multiPointerRejectedWhenCapabilityFalse() {
        val result = GestureRunner.validatePointers(
            listOf(tapPath("f1"), tapPath("f2")),
            singlePointerCaps,
        )
        assertNotNull(result)
        assertTrue(
            "expected multi-pointer rejection, got: $result",
            result!!.contains("canMultiPointer=false"),
        )
    }

    @Test
    fun pointerWithNoWaypointsRejected() {
        val result = GestureRunner.validatePointers(
            listOf(PointerPath("empty", emptyList())),
            singlePointerCaps,
        )
        assertEquals("pointer 'empty' has no waypoints", result)
    }

    @Test
    fun pointerWithOneWaypointRejected() {
        val result = GestureRunner.validatePointers(
            listOf(PointerPath("one", listOf(wp("down", offset = 0.0)))),
            singlePointerCaps,
        )
        assertNotNull(result)
        assertTrue(result!!.contains("at least 2 waypoints"))
    }

    @Test
    fun missingOpeningDownRejected() {
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(wp("move", offset = 0.0), wp("up", offset = 0.05)),
            ),
        )
        val result = GestureRunner.validatePointers(pointers, singlePointerCaps)
        assertNotNull(result)
        assertTrue(result!!.contains("must start with phase=down"))
    }

    @Test
    fun missingClosingUpRejected() {
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(wp("down", offset = 0.0), wp("move", offset = 0.05)),
            ),
        )
        val result = GestureRunner.validatePointers(pointers, singlePointerCaps)
        assertNotNull(result)
        assertTrue(result!!.contains("must end with phase=up"))
    }

    @Test
    fun nonMonotonicOffsetsRejected() {
        // `StrokeDescription` requires monotonically non-decreasing
        // waypoint offsets; a backwards offset would otherwise
        // surface as an `IllegalArgumentException` inside the
        // platform builder. Catch at the validator so the error is
        // structured and readable.
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(
                    wp("down", offset = 0.0),
                    wp("move", offset = 0.2),
                    wp("up", offset = 0.1),
                ),
            ),
        )
        val result = GestureRunner.validatePointers(pointers, singlePointerCaps)
        assertNotNull(result)
        assertTrue(result!!.contains("monotonically"))
    }

    @Test
    fun pressureRejectedWhenCapabilityFalse() {
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(
                    wp("down", offset = 0.0, pressure = 0.5),
                    wp("up", offset = 0.05),
                ),
            ),
        )
        val result = GestureRunner.validatePointers(pointers, singlePointerCaps)
        assertEquals("pressure rejected: driver reports canPressure=false", result)
    }

    @Test
    fun pressureOutOfRangeRejected() {
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(
                    wp("down", offset = 0.0, pressure = 1.5),
                    wp("up", offset = 0.05),
                ),
            ),
        )
        val result = GestureRunner.validatePointers(pointers, pressureCaps)
        assertNotNull(result)
        assertTrue(
            "expected pressure OOR rejection, got: $result",
            result!!.contains("outside valid range"),
        )
    }

    @Test
    fun negativeCoordinatesRejected() {
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(
                    wp("down", x = -5f, offset = 0.0),
                    wp("up", offset = 0.05),
                ),
            ),
        )
        val result = GestureRunner.validatePointers(pointers, singlePointerCaps)
        assertNotNull(result)
        assertTrue(result!!.contains("non-negative"))
    }

    @Test
    fun equalAdjacentOffsetsAccepted() {
        // Monotonic-non-decreasing is the contract, not strictly
        // increasing. A zero-duration tap (down + up at the same
        // offset) must pass validation; the MIN_PRESS_MS floor in
        // buildStroke handles the platform duration minimum.
        val pointers = listOf(
            PointerPath(
                "f1",
                listOf(wp("down", offset = 0.0), wp("up", offset = 0.0)),
            ),
        )
        assertNull(GestureRunner.validatePointers(pointers, singlePointerCaps))
    }
}
