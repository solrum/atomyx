package dev.atomyx.agent.control

/**
 * Dispatches coordinate-level pointer gestures through the
 * `GestureRunner` pipeline. Every primitive builds a single-pointer
 * `PointerPath` and delegates — no bespoke `dispatchGesture` paths
 * remain, so validation, completion waits, and the
 * `DispatchResult` shape are identical across tap / swipe /
 * long-press / raw `dispatchGesture`.
 *
 * Responsibility boundary:
 *
 *   - **Gesture primitives only**. Text input helpers live in
 *     [InputDispatcher]; global key presses / app lifecycle live
 *     in [SystemActionDispatcher]. A reader who only needs to
 *     understand "how does a tap reach the OS" should not have
 *     to scroll past typing heuristics.
 */
class GestureDispatcher(
    private val runner: GestureRunner,
) {

    /**
     * Tap at the given screen coordinates. Blocks until the
     * accessibility service fires `GestureResultCallback` so a
     * caller that immediately dumps the tree sees post-tap state
     * instead of racing the gesture — no fire-and-forget path
     * survives the dispatch pipeline.
     */
    fun tapAt(x: Float, y: Float): GestureRunner.DispatchResult {
        val pointers = listOf(
            GestureRunner.PointerPath(
                id = "tap",
                waypoints = listOf(
                    GestureRunner.Waypoint("down", x, y, 0.0, null),
                    GestureRunner.Waypoint("up", x, y, 0.0, null),
                ),
            ),
        )
        return runner.dispatch(pointers)
    }

    /**
     * Linear swipe between two coordinates over `durationMs`.
     * Blocks until completion for the same race-avoidance reason
     * as [tapAt]. `MIN_PRESS_MS` floor in `GestureRunner.buildStroke`
     * keeps a zero-duration swipe inside `StrokeDescription`'s
     * `duration > 0` precondition.
     */
    fun swipe(
        fromX: Float,
        fromY: Float,
        toX: Float,
        toY: Float,
        durationMs: Long,
    ): GestureRunner.DispatchResult {
        val durationSec = durationMs.coerceAtLeast(0L) / 1000.0
        val pointers = listOf(
            GestureRunner.PointerPath(
                id = "swipe",
                waypoints = listOf(
                    GestureRunner.Waypoint("down", fromX, fromY, 0.0, null),
                    GestureRunner.Waypoint("up", toX, toY, durationSec, null),
                ),
            ),
        )
        return runner.dispatch(pointers)
    }

    /**
     * Press-and-hold at the given coordinates for `durationMs`.
     * Duration is coerced to [300, 5000]ms to stay in the "long
     * press" gesture recogniser window on every known Android
     * version — below 300ms the OS reads a tap, above 5s the
     * context menu animation can interfere with subsequent taps.
     */
    fun longPressAt(
        x: Float,
        y: Float,
        durationMs: Long = 800L,
    ): GestureRunner.DispatchResult {
        val d = durationMs.coerceIn(300L, 5000L) / 1000.0
        val pointers = listOf(
            GestureRunner.PointerPath(
                id = "longPress",
                waypoints = listOf(
                    GestureRunner.Waypoint("down", x, y, 0.0, null),
                    GestureRunner.Waypoint("up", x, y, d, null),
                ),
            ),
        )
        return runner.dispatch(pointers)
    }
}
