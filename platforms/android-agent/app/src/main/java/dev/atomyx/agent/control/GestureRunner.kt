package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.os.Build
import dev.atomyx.agent.service.AtomyxAccessibilityService
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
 *     pointer stays stationary, the only shape of input that
 *     satisfies Flutter's long-press-first drag recognisers.
 *   - Multi-pointer paths (pinch, rotate, two-finger swipe) run
 *     through [dispatchMultiPointer]: the same one-`GestureDescription`-
 *     per-time-segment chaining as the single-pointer path, except
 *     each segment holds one continued stroke per pointer still
 *     active in it. Packing every pointer's sub-strokes into one
 *     `GestureDescription` instead (as a single stroke-count-capped
 *     description covering the whole gesture) loses angular fidelity
 *     across the packed sub-stroke boundaries, which is why a rotate
 *     built that way reads as no rotation at all; segmenting by time
 *     keeps the sweep intact and keeps every description's stroke
 *     count at the pointer count, well under the platform cap on
 *     every supported API level.
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
    // Defaults to the real accessibility screenshot surface; a test
    // substitutes a fake here to exercise capture scheduling without
    // an AccessibilityService.
    private val screenshotCapture: () -> ByteArray? = {
        (service as? AtomyxAccessibilityService)?.takeScreenshotJpeg()
    },
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
             *     current Android level, so it is a permanent `false`. The
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
     * Outcome of [dispatch]: the gesture's own completion result plus
     * every screenshot taken at a `capture` waypoint, in the order
     * those waypoints appeared across the dispatched pointers.
     * [capturedImages] is empty when the gesture carried no `capture`
     * waypoint.
     */
    data class DispatchOutcome(
        val result: DispatchResult,
        val capturedImages: List<ByteArray> = emptyList(),
    )

    /**
     * Validate + dispatch. Single-pointer gestures run through
     * the multi-phase dispatcher, where each waypoint segment becomes
     * its own `GestureDescription` with `willContinue=true`
     * chaining, which keeps the pointer DOWN between dispatch
     * calls per the `AccessibilityService` contract. This lets
     * a "hold" segment be expressed as silent Kotlin-level sleep
     * between dispatches (no `ACTION_MOVE` events emitted),
     * which is the only way to satisfy Flutter's
     * `DelayedMultiDragGestureRecognizer`, because it rejects the
     * gesture if ANY move event arrives before its long-press
     * timer fires, regardless of delta magnitude. A single
     * `GestureDescription` with a zero-length hold stroke emits
     * periodic same-coord `ACTION_MOVE` events during the hold
     * (Android interpolates at ~10ms inside every stroke's
     * active window), which arms `onLongPress` but not
     * `onReorder`.
     *
     * Multi-pointer gestures dispatch through [dispatchMultiPointer],
     * which chains one `GestureDescription` per time segment across
     * every still-active pointer, the same lockstep-across-dispatch
     * shape [dispatchMultiPhase] uses for a single pointer.
     * Reorder-style multi-pointer is not a real use case; the
     * multi-phase hold-then-drag is single-pointer by shape.
     *
     * A `capture` waypoint carries no motion or stroke of its own; it
     * marks a point in time, strictly between a pointer's opening
     * `down` and closing `up`, where a screenshot is taken while the
     * touch is still held. Every `capture` waypoint across all
     * pointers is stripped before the waypoints below reach the
     * stroke builders, and a background thread per waypoint sleeps
     * until its own `atOffsetSeconds` elapses (measured from the top
     * of this call, the same clock the stroke dispatch below advances
     * against) before taking the screenshot. This runs concurrently
     * with the gesture dispatch rather than pausing it: Android's
     * continued-stroke gesture has no time-based continuation
     * timeout (the system holds the touch open until the next
     * `continueStroke` dispatch arrives, cancelling only when a real
     * `MotionEvent` interrupts it), and a screenshot injects no
     * `MotionEvent`, so it cannot cancel the held touch.
     */
    fun dispatch(pointers: List<PointerPath>): DispatchOutcome {
        val validation = validate(pointers)
        if (validation != null) return DispatchOutcome(DispatchResult.Rejected(validation))

        val captureOffsetsSeconds = captureOffsets(pointers)
        val captured = arrayOfNulls<ByteArray>(captureOffsetsSeconds.size)
        val captureLatch = scheduleCaptures(captureOffsetsSeconds, captured)

        val strippedPointers = pointers.map { ptr ->
            ptr.copy(waypoints = ptr.waypoints.filter { it.phase != PHASE_CAPTURE })
        }
        val result = if (strippedPointers.size == 1) {
            dispatchMultiPhase(strippedPointers[0])
        } else {
            dispatchMultiPointer(strippedPointers)
        }

        captureLatch?.await(CAPTURE_TIMEOUT_MS, TimeUnit.MILLISECONDS)
        return DispatchOutcome(result, captured.map { it ?: ByteArray(0) })
    }

    /**
     * Fires one daemon thread per capture offset, each sleeping until
     * its own offset elapses before invoking [screenshotCapture] and
     * recording the result at its index in [results]. Returns a latch
     * that reaches zero once every thread has recorded a result (or
     * null when there are no offsets to schedule), so [dispatch] can
     * bound how long it waits for outstanding captures once the
     * gesture itself has finished.
     */
    private fun scheduleCaptures(
        offsetsSeconds: List<Double>,
        results: Array<ByteArray?>,
    ): CountDownLatch? {
        if (offsetsSeconds.isEmpty()) return null
        val latch = CountDownLatch(offsetsSeconds.size)
        for ((index, offsetSeconds) in offsetsSeconds.withIndex()) {
            val delayMs = (offsetSeconds * 1000.0).toLong().coerceAtLeast(0L)
            Thread({
                try {
                    if (delayMs > 0) Thread.sleep(delayMs)
                    results[index] = screenshotCapture()
                } catch (_: InterruptedException) {
                    // Leave results[index] null; dispatch() maps a
                    // missing capture to an empty byte array rather
                    // than blocking indefinitely.
                } finally {
                    latch.countDown()
                }
            }, "atomyx-gesture-capture-$index").apply { isDaemon = true }.start()
        }
        return latch
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

        // `handler = null` means the callback is delivered on the service's
        // main thread. Safe because our caller is on a NanoHTTPD
        // worker thread, never main. See the threading note in
        // the class doc.
        val accepted = service.dispatchGesture(gesture, callback, null)
        if (!accepted) {
            return DispatchResult.Rejected(
                "service.dispatchGesture returned false: accessibility " +
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
     *     gesture has completed, but the pointer stays DOWN
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

    /**
     * Dispatch two or more pointers as a chain of `GestureDescription`s,
     * one per time segment, each holding one continued
     * `StrokeDescription` per pointer still active in that segment.
     * This is [dispatchMultiPhase]'s lockstep-across-dispatch template
     * generalized from one pointer to N: every pointer's stroke for a
     * given segment rides in the same dispatch, so concurrent pointers
     * stay in lockstep, and no single description ever holds more than
     * `pointers.size` strokes.
     *
     * [planMultiPointerSegments] carries a continued stroke's start
     * point forward as the previous stroke's end point for the same
     * pointer; the platform requires that equality to accept the
     * continuation, so this method only has to thread the previous
     * [GestureDescription.StrokeDescription] per pointer index across
     * segments, exactly as [dispatchMultiPhase] threads its single one.
     */
    private fun dispatchMultiPointer(pointers: List<PointerPath>): DispatchResult {
        val prevStrokes = arrayOfNulls<GestureDescription.StrokeDescription>(pointers.size)

        for (segment in planMultiPointerSegments(pointers)) {
            if (segment.pointerSegments.isEmpty()) continue

            val builder = GestureDescription.Builder()
            for (seg in segment.pointerSegments) {
                val path = Path().apply {
                    moveTo(seg.fromX, seg.fromY)
                    lineTo(seg.toX, seg.toY)
                }
                val prevStroke = prevStrokes[seg.pointerIndex]
                val stroke = if (prevStroke == null) {
                    GestureDescription.StrokeDescription(
                        path, 0L, seg.strokeDurationMs, seg.willContinue,
                    )
                } else {
                    prevStroke.continueStroke(
                        path, 0L, seg.strokeDurationMs, seg.willContinue,
                    )
                }
                builder.addStroke(stroke)
                prevStrokes[seg.pointerIndex] = stroke
            }

            val phaseResult = dispatchSingleGesture(builder.build())
            if (phaseResult !is DispatchResult.Completed) return phaseResult

            if (segment.postSleepMs > 0) {
                try {
                    Thread.sleep(segment.postSleepMs)
                } catch (_: InterruptedException) {
                    return DispatchResult.Cancelled
                }
            }
        }
        return DispatchResult.Completed
    }

    companion object {
        /**
         * Waypoint phase marking a point strictly between a pointer's
         * opening `down` and closing `up` where a screenshot is taken
         * while the touch is still held. Carries no motion of its
         * own; see the `dispatch` doc for how it is scheduled.
         */
        const val PHASE_CAPTURE = "capture"

        /**
         * Bounds how long [dispatch] waits for outstanding capture
         * threads after the gesture dispatch itself has finished.
         * Each capture is scheduled to fire well inside the gesture's
         * own hold, so this bound only guards against a
         * pathologically slow screenshot call.
         */
        const val CAPTURE_TIMEOUT_MS = 10_000L

        /**
         * Collects every `capture` waypoint's `atOffsetSeconds`,
         * walking pointers and their waypoints in dispatch order so
         * the result lines up with the order [dispatch]'s caller
         * expects captured images back in. Extracted as a pure
         * function, like [validatePointers], so unit tests can pin
         * the ordering without an `AccessibilityService`.
         */
        fun captureOffsets(pointers: List<PointerPath>): List<Double> {
            val offsets = mutableListOf<Double>()
            for (ptr in pointers) {
                for (w in ptr.waypoints) {
                    if (w.phase == PHASE_CAPTURE) offsets.add(w.atOffsetSeconds)
                }
            }
            return offsets
        }

        /**
         * Minimum stroke duration in ms. Floor applied when the
         * author supplies a zero-duration segment, satisfying
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

        /**
         * One pointer's contribution to one time segment: the
         * straight-line path endpoints, the stroke duration to issue
         * for it, and whether that pointer's touch stays down once
         * this segment's `GestureDescription` completes.
         */
        data class PointerSegment(
            val pointerIndex: Int,
            val fromX: Float,
            val fromY: Float,
            val toX: Float,
            val toY: Float,
            val strokeDurationMs: Long,
            val willContinue: Boolean,
        )

        /**
         * One `dispatchGesture` call's worth of work: every pointer's
         * stroke for this time segment, plus the Kotlin-level sleep to
         * add once the segment's `GestureDescription` completes so the
         * segment's real-world duration matches whichever pointer
         * authored the longest duration for it. The sleep is only
         * nonzero when every pointer active in the segment issued a
         * shorter stroke than its authored duration (the hold-segment
         * case); when even one pointer's stroke runs the full authored
         * duration, `dispatchGesture` already blocks that long waiting
         * for it to finish.
         */
        data class DispatchSegment(
            val pointerSegments: List<PointerSegment>,
            val postSleepMs: Long,
        )

        /**
         * Pure planner: turns N pointers' waypoint lists into an
         * ordered list of [DispatchSegment]s, one per `dispatchGesture`
         * call, generalizing [dispatchMultiPhase]'s one-pointer
         * chaining to N pointers. For `pointers.size == 1` this
         * produces the identical stroke durations, continuation flags,
         * and sleep as the single-pointer path, segment for segment.
         * Extracted as a pure function, like [validatePointers], so
         * unit tests can pin segment counts, durations, and
         * continuation flags without an `AccessibilityService`.
         *
         * A pointer whose waypoint list is shorter than another's
         * finishes (its last segment carries `willContinue=false`) at
         * its own final segment and is simply absent from every
         * segment after that; the platform has already released that
         * touch, so there is nothing left to continue.
         */
        fun planMultiPointerSegments(pointers: List<PointerPath>): List<DispatchSegment> {
            val segmentCount = pointers.maxOf { it.waypoints.size - 1 }
            val segments = mutableListOf<DispatchSegment>()

            for (segmentIndex in 1..segmentCount) {
                val pointerSegments = mutableListOf<PointerSegment>()
                var intendedDurationMs = 0L
                var issuedDurationMs = 0L

                for ((pointerIndex, ptr) in pointers.withIndex()) {
                    val waypoints = ptr.waypoints
                    if (segmentIndex >= waypoints.size) continue

                    val prev = waypoints[segmentIndex - 1]
                    val cur = waypoints[segmentIndex]
                    val prevOffsetMs = (prev.atOffsetSeconds * 1000.0).toLong().coerceAtLeast(0L)
                    val curOffsetMs = (cur.atOffsetSeconds * 1000.0).toLong()
                        .coerceAtLeast(prevOffsetMs)
                    val segAuthorDurationMs = curOffsetMs - prevOffsetMs
                    val isLastForPointer = segmentIndex == waypoints.size - 1
                    val isZeroLength = prev.x == cur.x && prev.y == cur.y

                    // Hold segments cap the issued stroke at MIN_PRESS_MS so the
                    // platform does not spam same-coordinate ACTION_MOVE events for
                    // the segment's full authored duration; the remainder is made up
                    // by postSleepMs below (or, when another pointer's stroke in this
                    // same segment runs longer, by dispatchGesture's own blocking
                    // wait for that pointer to finish).
                    val strokeDurationMs = if (isZeroLength) {
                        MIN_PRESS_MS
                    } else {
                        segAuthorDurationMs.coerceAtLeast(MIN_PRESS_MS)
                    }

                    pointerSegments.add(
                        PointerSegment(
                            pointerIndex,
                            prev.x, prev.y, cur.x, cur.y,
                            strokeDurationMs, !isLastForPointer,
                        ),
                    )
                    intendedDurationMs = maxOf(intendedDurationMs, segAuthorDurationMs)
                    issuedDurationMs = maxOf(issuedDurationMs, strokeDurationMs)
                }

                segments.add(
                    DispatchSegment(
                        pointerSegments,
                        (intendedDurationMs - issuedDurationMs).coerceAtLeast(0L),
                    ),
                )
            }
            return segments
        }
    }
}
