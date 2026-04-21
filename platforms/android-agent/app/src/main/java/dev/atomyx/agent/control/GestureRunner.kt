package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.os.Build
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * Gesture dispatch pipeline backed by
 * `AccessibilityService.dispatchGesture`. Accepts a pointer list
 * (one or more pointers, each with an ordered waypoint sequence
 * carrying phase / coordinate / offset) and blocks until the
 * service reports completion or the deadline elapses.
 *
 * Dispatch shape:
 *
 *   - Single-pointer paths run through [dispatchMultiPhase]: one
 *     `GestureDescription` per adjacent waypoint pair, chained
 *     via `continueStroke(willContinue=true)` across separate
 *     `dispatchGesture` calls. Hold segments (adjacent same-
 *     coordinate waypoints) emit a minimal stroke then sleep the
 *     remainder at Kotlin level so no events flow while the
 *     pointer stays stationary — the only shape of input that
 *     satisfies Flutter's long-press-first drag recognisers.
 *   - Multi-pointer paths (pinch, rotate, two-finger swipe) run
 *     through a single `GestureDescription` so concurrent timing
 *     stays honoured across pointers.
 *
 * Threading contract:
 *
 *   Callers MUST NOT invoke `dispatch` from the service's main
 *   thread. `AccessibilityService.dispatchGesture` delivers its
 *   `GestureResultCallback` on main; calling from main would
 *   block main waiting for a latch that only main can fire,
 *   stalling for the full 30s budget. HTTP route handlers on
 *   NanoHTTPD's worker pool are the supported entry point.
 */
class GestureRunner(
    private val service: AccessibilityService,
    private val capabilities: GestureCapabilities = GestureCapabilities.DEFAULT,
) {

    /**
     * Expose a read-only copy of the current capability flags so
     * the `/ping` route can surface them to the host. Returning a
     * `copy()` keeps the runner's internal state immutable from
     * outside the class.
     */
    fun capabilitiesSnapshot(): GestureCapabilities = capabilities.copy()

    data class GestureCapabilities(
        val canMultiPointer: Boolean,
        val canPressure: Boolean,
    ) {
        companion object {
            /**
             * Runtime-derived defaults.
             *
             *   - `canMultiPointer`: `GestureDescription.Builder`
             *     accepts concurrent strokes at the app's `minSdk`
             *     and above. Kept as a version check rather than a
             *     literal so a future API regression surfaces as a
             *     capability flag drop instead of a silent dispatch
             *     failure at runtime.
             *
             *   - `canPressure`: the `AccessibilityService` gesture
             *     surface exposes no per-touch pressure API on any
             *     current Android level — permanent `false`. The
             *     one place to flip if the platform ever adds a
             *     pressure-carrying stroke constructor.
             */
            val DEFAULT = GestureCapabilities(
                canMultiPointer = Build.VERSION.SDK_INT >= Build.VERSION_CODES.N,
                canPressure = false,
            )
        }
    }

    data class Waypoint(
        val phase: String,
        val x: Float,
        val y: Float,
        val atOffsetSeconds: Double,
        val pressure: Double?,
    )

    data class PointerPath(
        val id: String,
        val waypoints: List<Waypoint>,
    )

    sealed class DispatchResult {
        object Completed : DispatchResult()
        object Cancelled : DispatchResult()
        data class TimedOut(val waitedMs: Long) : DispatchResult()
        data class Rejected(val reason: String) : DispatchResult()
    }

    /**
     * Validate + dispatch. Single-pointer gestures run through
     * the multi-phase dispatcher — each waypoint segment becomes
     * its own `GestureDescription` with `willContinue=true`
     * chaining, which keeps the pointer DOWN between dispatch
     * calls per the `AccessibilityService` contract. This lets
     * a "hold" segment be expressed as silent Kotlin-level sleep
     * between dispatches (no `ACTION_MOVE` events emitted),
     * which is the only way to satisfy Flutter's
     * `DelayedMultiDragGestureRecognizer` — it rejects the
     * gesture if ANY move event arrives before its long-press
     * timer fires, regardless of delta magnitude. A single
     * `GestureDescription` with a zero-length hold stroke emits
     * periodic same-coord `ACTION_MOVE` events during the hold
     * (Android interpolates at ~10ms inside every stroke's
     * active window), which arms `onLongPress` but not
     * `onReorder`.
     *
     * Multi-pointer gestures use the single-dispatch path —
     * each pointer's segments stay in the same
     * `GestureDescription` so concurrent timing is honoured.
     * Reorder-style multi-pointer is not a real use case; the
     * multi-phase hold-then-drag is single-pointer by shape.
     */
    fun dispatch(pointers: List<PointerPath>): DispatchResult {
        val validation = validate(pointers)
        if (validation != null) return DispatchResult.Rejected(validation)

        if (pointers.size == 1) {
            return dispatchMultiPhase(pointers[0])
        }
        return dispatchSingleGesture(buildGesture(pointers))
    }

    private fun dispatchSingleGesture(gesture: GestureDescription): DispatchResult {
        val latch = CountDownLatch(1)
        val outcome = AtomicReference<DispatchResult>(DispatchResult.Completed)

        val callback = object : AccessibilityService.GestureResultCallback() {
            override fun onCompleted(g: GestureDescription?) {
                outcome.set(DispatchResult.Completed)
                latch.countDown()
            }

            override fun onCancelled(g: GestureDescription?) {
                outcome.set(DispatchResult.Cancelled)
                latch.countDown()
            }
        }

        // `handler = null` → callback delivered on the service's
        // main thread. Safe because our caller is on a NanoHTTPD
        // worker thread, never main; see the threading note in
        // the class doc.
        val accepted = service.dispatchGesture(gesture, callback, null)
        if (!accepted) {
            return DispatchResult.Rejected(
                "service.dispatchGesture returned false — accessibility " +
                    "service disconnected or gesture rejected before queueing",
            )
        }

        val finished = latch.await(GESTURE_TIMEOUT_MS, TimeUnit.MILLISECONDS)
        return if (finished) outcome.get() else DispatchResult.TimedOut(GESTURE_TIMEOUT_MS)
    }

    /**
     * Dispatch a single-pointer path as a chain of
     * `GestureDescription`s linked by
     * `continueStroke(willContinue=true)`. Each adjacent waypoint
     * pair becomes one dispatch:
     *
     *   - Hold segment (same-coordinate waypoints): emit a
     *     minimal `MIN_PRESS_MS` stroke then `Thread.sleep` the
     *     remainder. No events flow during the sleep because the
     *     gesture has completed — but the pointer stays DOWN
     *     because `willContinue=true` was set on the stroke.
     *   - Move segment (distinct coordinates): emit a full-
     *     duration stroke so the platform interpolates
     *     `ACTION_MOVE` events uniformly along the path.
     *
     * Contract from `AccessibilityService.dispatchGesture`:
     *
     *   - `willContinue=true` on the final stroke of a
     *     `GestureDescription` keeps the pointer DOWN after the
     *     gesture completes.
     *   - The next dispatch's `continueStroke` extends the SAME
     *     touch: one `ACTION_DOWN` at the start, one `ACTION_UP`
     *     after the final stroke with `willContinue=false`.
     *
     * Why the hold-as-sleep pattern matters:
     *
     *   A single `GestureDescription` whose Path contains a
     *   zero-length segment still emits periodic same-coordinate
     *   `ACTION_MOVE` events inside that segment's active window
     *   (the platform interpolates at ~10ms regardless of path
     *   length). Some gesture recognisers (Flutter's
     *   `DelayedMultiDragGestureRecognizer`, which backs
     *   `ReorderableListView`) reject on the first MOVE event
     *   regardless of delta magnitude. Expressing the hold as a
     *   Kotlin sleep between dispatches keeps the silent window
     *   truly silent and lets the long-press timer fire.
     */
    private fun dispatchMultiPhase(ptr: PointerPath): DispatchResult {
        var prevStroke: GestureDescription.StrokeDescription? = null
        for (i in 1 until ptr.waypoints.size) {
            val prev = ptr.waypoints[i - 1]
            val cur = ptr.waypoints[i]
            val prevOffsetMs = (prev.atOffsetSeconds * 1000.0).toLong().coerceAtLeast(0L)
            val curOffsetMs = (cur.atOffsetSeconds * 1000.0).toLong().coerceAtLeast(prevOffsetMs)
            val segAuthorDurationMs = curOffsetMs - prevOffsetMs
            val isLast = i == ptr.waypoints.size - 1
            val isZeroLength = prev.x == cur.x && prev.y == cur.y

            val strokeDurationMs: Long
            val postSleepMs: Long
            if (isZeroLength) {
                // Hold: dispatch a minimal stroke then sleep the
                // remainder. During the sleep Android emits no
                // events (gesture already completed) but the
                // pointer stays down because this stroke has
                // `willContinue=true`.
                strokeDurationMs = MIN_PRESS_MS
                postSleepMs = (segAuthorDurationMs - MIN_PRESS_MS).coerceAtLeast(0L)
            } else {
                // Real movement: full duration so Android
                // interpolates ACTION_MOVE events.
                strokeDurationMs = segAuthorDurationMs.coerceAtLeast(MIN_PRESS_MS)
                postSleepMs = 0L
            }

            val path = Path().apply {
                moveTo(prev.x, prev.y)
                lineTo(cur.x, cur.y)
            }

            // `startTime=0` inside each single-stroke gesture;
            // absolute timing lives in the across-dispatch sleeps.
            val stroke = if (prevStroke == null) {
                GestureDescription.StrokeDescription(
                    path, 0L, strokeDurationMs, !isLast,
                )
            } else {
                prevStroke.continueStroke(path, 0L, strokeDurationMs, !isLast)
            }

            val gesture = GestureDescription.Builder().addStroke(stroke).build()
            val phaseResult = dispatchSingleGesture(gesture)
            if (phaseResult !is DispatchResult.Completed) return phaseResult

            if (postSleepMs > 0) {
                try {
                    Thread.sleep(postSleepMs)
                } catch (_: InterruptedException) {
                    return DispatchResult.Cancelled
                }
            }
            prevStroke = stroke
        }
        return DispatchResult.Completed
    }

    private fun validate(pointers: List<PointerPath>): String? =
        validatePointers(pointers, capabilities)

    private fun buildGesture(pointers: List<PointerPath>): GestureDescription {
        val builder = GestureDescription.Builder()
        for (ptr in pointers) {
            for (stroke in buildStrokes(ptr)) {
                builder.addStroke(stroke)
            }
        }
        return builder.build()
    }

    /**
     * Build a chain of `StrokeDescription`s from one pointer's
     * waypoints — ONE stroke per adjacent waypoint pair — linked
     * via `continueStroke(willContinue = true)` so the platform
     * preserves author-specified per-segment timing.
     *
     * Why this is not a single stroke with a composite Path:
     *
     *   `AccessibilityService.dispatchGesture` interpolates along
     *   a single stroke's Path uniformly over `duration`. Author
     *   `atOffsetSeconds` values between intermediate waypoints
     *   are lost — a pointer that should hold for 1500ms and then
     *   drag for 800ms becomes a uniform slide over 2300ms. Flutter
     *   `DelayedMultiDragGestureRecognizer` (used by
     *   `ReorderableListView`) requires the pointer to stay
     *   stationary for `kLongPressTimeout` (500ms) before arming;
     *   a uniform slide never satisfies that. The recognizer
     *   never engages and the reorder silently fails.
     *
     * One stroke per segment lets Android honour each segment's
     * duration distinctly — a stationary segment stays stationary
     * for its full duration, a moving segment moves for its full
     * duration. Chained via `continueStroke(willContinue=true)`,
     * Android treats the sequence as one continuous touch from
     * the pointer's perspective — gesture recognisers see
     * `ACTION_DOWN` once, a stream of `ACTION_MOVE`, then one
     * `ACTION_UP`.
     *
     * `MIN_PRESS_MS` floor is applied per-segment so a zero-gap
     * between adjacent waypoints (e.g. `[down(0), up(0)]`) still
     * produces a stroke with `duration > 0` — satisfies
     * `StrokeDescription`'s precondition without altering author
     * intent for longer segments.
     */
    private fun buildStrokes(ptr: PointerPath): List<GestureDescription.StrokeDescription> {
        val strokes = mutableListOf<GestureDescription.StrokeDescription>()
        var prev = ptr.waypoints.first()
        var prevStartMs = (prev.atOffsetSeconds * 1000.0).toLong().coerceAtLeast(0L)

        for (i in 1 until ptr.waypoints.size) {
            val cur = ptr.waypoints[i]
            val curOffsetMs = (cur.atOffsetSeconds * 1000.0).toLong()
            val segDurationMs = (curOffsetMs - prevStartMs).coerceAtLeast(MIN_PRESS_MS)
            val isLast = i == ptr.waypoints.size - 1

            val path = Path().apply {
                moveTo(prev.x, prev.y)
                lineTo(cur.x, cur.y)
            }

            val stroke = if (strokes.isEmpty()) {
                GestureDescription.StrokeDescription(
                    path, prevStartMs, segDurationMs, !isLast,
                )
            } else {
                strokes.last().continueStroke(
                    path, prevStartMs, segDurationMs, !isLast,
                )
            }
            strokes.add(stroke)

            prev = cur
            prevStartMs += segDurationMs
        }
        return strokes
    }

    companion object {
        /**
         * Minimum stroke duration in ms. Floor applied when the
         * author supplies a zero-duration segment — satisfies
         * `StrokeDescription`'s `duration > 0` precondition while
         * keeping the 50ms value small enough that a tap registers
         * as a tap rather than a long-press.
         */
        const val MIN_PRESS_MS = 50L

        /**
         * Completion wait budget per single-dispatch phase. Covers
         * pathological cases like a long press-and-drag (~1.5s
         * active time plus a small inertia-settle margin) with
         * head-room for slower devices.
         */
        const val GESTURE_TIMEOUT_MS = 30_000L

        /**
         * Pure validation for a pointer batch. Returns null when
         * the batch is safe to dispatch, otherwise a human-readable
         * rejection reason. Extracted so unit tests can exercise
         * every rejection path without an `AccessibilityService`.
         */
        fun validatePointers(
            pointers: List<PointerPath>,
            capabilities: GestureCapabilities,
        ): String? {
            if (pointers.isEmpty()) return "empty pointers array"
            if (pointers.size > 1 && !capabilities.canMultiPointer) {
                return "multi-pointer gesture rejected: driver reports " +
                    "canMultiPointer=false (received ${pointers.size} pointers)"
            }
            for (ptr in pointers) {
                if (ptr.waypoints.isEmpty()) {
                    return "pointer '${ptr.id}' has no waypoints"
                }
                if (ptr.waypoints.size < 2) {
                    return "pointer '${ptr.id}' must have at least 2 waypoints (down + up)"
                }
                val first = ptr.waypoints.first()
                if (first.phase != "down") {
                    return "pointer '${ptr.id}' must start with phase=down, got '${first.phase}'"
                }
                val last = ptr.waypoints.last()
                if (last.phase != "up") {
                    return "pointer '${ptr.id}' must end with phase=up, got '${last.phase}'"
                }
                var prevOffset = Double.NEGATIVE_INFINITY
                for (w in ptr.waypoints) {
                    if (w.atOffsetSeconds < prevOffset) {
                        return "pointer '${ptr.id}' waypoint offsets must be monotonically " +
                            "non-decreasing (got ${w.atOffsetSeconds}s after ${prevOffset}s)"
                    }
                    prevOffset = w.atOffsetSeconds
                    if (w.pressure != null) {
                        if (!capabilities.canPressure) {
                            return "pressure rejected: driver reports canPressure=false"
                        }
                        if (w.pressure < 0.0 || w.pressure > 1.0) {
                            return "pressure ${w.pressure} outside valid range [0.0, 1.0]"
                        }
                    }
                    if (w.x < 0f || w.y < 0f) {
                        return "coordinates must be non-negative (got ${w.x}, ${w.y})"
                    }
                }
            }
            return null
        }
    }
}
