package dev.atomyx.agent.control.router

import com.google.gson.Gson
import com.google.gson.JsonObject
import dev.atomyx.agent.control.AtomyxServices
import dev.atomyx.agent.control.GestureRunner
import fi.iki.elonen.NanoHTTPD

private val gson = Gson()

private fun toJson(payload: Any) = gson.toJson(payload)

private fun parseBody(body: String): JsonObject =
    runCatching { gson.fromJson(body, JsonObject::class.java) }.getOrNull() ?: JsonObject()

// ────────────────────────────────────────────────────────────────────
// Read-only routes
// ────────────────────────────────────────────────────────────────────

/**
 * Agent handshake. Returns capabilities + the name of the active
 * gesture mechanism so the host-side driver can populate its
 * `Capabilities` before the YAML validator runs. Called once at
 * `AndroidDriver.connect()` after `/health`.
 *
 * Response shape:
 * ```
 * {
 *   "ok": true,
 *   "agent": "atomyx-android",
 *   "sdkInt": 34,
 *   "mechanism": "accessibility",
 *   "capabilities": {
 *     "canMultiPointer": false,
 *     "canPressure": false
 *   }
 * }
 * ```
 *
 */
class PingRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/ping"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val caps = services.gestureRunner.capabilitiesSnapshot()
        return RouteResponse.ok(toJson(mapOf(
            "ok" to true,
            "agent" to "atomyx-android",
            "sdkInt" to android.os.Build.VERSION.SDK_INT,
            // `accessibility` names the one current mechanism — the
            // AccessibilityService gesture surface. Future backends
            // (e.g. a UiAutomation-based injector) would announce
            // themselves here without changing the wire shape.
            "mechanism" to "accessibility",
            "capabilities" to mapOf(
                "canMultiPointer" to caps.canMultiPointer,
                "canPressure" to caps.canPressure,
            ),
        )))
    }
}

class TreeRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/tree"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        // Always returns the hierarchical RawElementDto tree.
        // `dumpCompact` stays on UiTreeService as an internal helper
        // for GestureDispatcher's on-screen-key fallback but is not
        // exposed on the wire — the host consumes the hierarchical
        // form exclusively.
        val payload = services.uiTree.dumpTree()
        return RouteResponse.ok(toJson(payload))
    }
}

class KeyboardRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/keyboard"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val info = services.uiTree.getKeyboardInfo()
        return RouteResponse.ok(toJson(mapOf(
            "visible" to info.visible,
            "packageName" to info.packageName,
            "layout" to info.layout,
            "bounds" to info.bounds?.let {
                mapOf("left" to it.left, "top" to it.top, "right" to it.right, "bottom" to it.bottom)
            },
            "keys" to info.keys.map {
                mapOf(
                    "label" to it.label,
                    "bounds" to mapOf(
                        "left" to it.bounds.left, "top" to it.bounds.top,
                        "right" to it.bounds.right, "bottom" to it.bounds.bottom,
                    ),
                )
            },
        )))
    }
}

class ScreenshotRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/screenshot"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val bytes = services.accessibility.takeScreenshotJpeg()
            ?: return RouteResponse.internalError("screenshot failed")
        val base64 = android.util.Base64.encodeToString(bytes, android.util.Base64.NO_WRAP)
        return RouteResponse.ok(toJson(mapOf("base64" to base64, "format" to "jpeg")))
    }
}

class CurrentActivityRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/current-activity"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse =
        RouteResponse.ok(toJson(services.accessibility.currentForegroundActivity()))
}

class AppsRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/apps"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val pm = services.accessibility.packageManager
        val apps = pm.getInstalledApplications(0).map {
            mapOf(
                "packageName" to it.packageName,
                "label" to pm.getApplicationLabel(it).toString(),
            )
        }
        return RouteResponse.ok(toJson(apps))
    }
}

// ────────────────────────────────────────────────────────────────────
// Action routes
// ────────────────────────────────────────────────────────────────────

/**
 * Shared serialiser for `GestureRunner.DispatchResult` → route
 * response map. Tap / swipe / long-press / dispatch_gesture all
 * reach the same runner pipeline now; they all surface the same
 * `code` set (`gesture_rejected` / `gesture_cancelled` /
 * `gesture_timed_out`) so the host can treat a single-tap failure
 * and a multi-pointer failure with one classifier.
 */
private fun dispatchResultToMap(
    result: GestureRunner.DispatchResult,
    extra: Map<String, Any?> = emptyMap(),
): Map<String, Any?> = when (result) {
    is GestureRunner.DispatchResult.Completed ->
        mapOf("ok" to true) + extra

    is GestureRunner.DispatchResult.Cancelled ->
        mapOf(
            "ok" to false,
            "reason" to "gesture cancelled by the accessibility service",
            "code" to "gesture_cancelled",
        )

    is GestureRunner.DispatchResult.TimedOut ->
        mapOf(
            "ok" to false,
            "reason" to "gesture completion not reported within ${result.waitedMs}ms",
            "code" to "gesture_timed_out",
        )

    is GestureRunner.DispatchResult.Rejected ->
        mapOf(
            "ok" to false,
            "reason" to result.reason,
            "code" to "gesture_rejected",
        )
}

class TapCoordsRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/tap_coords"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val result = services.gestures.tapAt(body.get("x").asFloat, body.get("y").asFloat)
        return RouteResponse.ok(toJson(dispatchResultToMap(result)))
    }
}

class LongPressRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/long_press"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val duration = body.get("durationMs")?.takeIf { !it.isJsonNull }?.asLong ?: 800L
        val result = services.gestures.longPressAt(
            body.get("x").asFloat,
            body.get("y").asFloat,
            duration,
        )
        return RouteResponse.ok(toJson(dispatchResultToMap(result)))
    }
}

class ClearFocusedInputRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/clear_focused_input"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val result = services.focusedInputClearer.clearFocusedInput()
        return RouteResponse.ok(toJson(mapOf("ok" to result.success, "reason" to result.reason)))
    }
}

class HideKeyboardRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/hide_keyboard"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val result = services.keyboardHider.hideKeyboard()
        return RouteResponse.ok(toJson(mapOf("ok" to result.success, "reason" to result.reason)))
    }
}

/**
 * Dispatch an arbitrary multi-waypoint pointer gesture. Accepts
 * one or more pointers, each with a time-ordered waypoint list
 * (phase / coordinate / offset, plus optional pressure).
 *
 * Request body:
 * ```
 * {
 *   "pointers": [
 *     {
 *       "id": "finger1",
 *       "waypoints": [
 *         { "phase": "down"|"move"|"up", "x": number, "y": number,
 *           "atOffsetSeconds": number, "pressure"?: number }
 *       ]
 *     }
 *   ]
 * }
 * ```
 *
 * Response shape:
 *   - `{"ok":true,"pointers":N}` on successful completion.
 *   - `{"ok":false,"reason":"...","code":"..."}` on rejection,
 *     cancellation, or timeout. `code` is one of
 *     `gesture_rejected` (validation or platform refusal),
 *     `gesture_cancelled` (accessibility service aborted),
 *     `gesture_timed_out` (completion not reported within the
 *     runner's 30s latch budget).
 */
class DispatchGestureRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/dispatch_gesture"

    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val pointers = parsePointers(body)
            ?: return RouteResponse.ok(toJson(mapOf(
                "ok" to false,
                "reason" to "invalid request body: `pointers` must be a non-empty array",
                "code" to "gesture_rejected",
            )))

        val result = services.gestureRunner.dispatch(pointers)
        return RouteResponse.ok(toJson(dispatchResultToMap(result, mapOf("pointers" to pointers.size))))
    }

    private fun parsePointers(body: JsonObject): List<GestureRunner.PointerPath>? {
        val arr = body.getAsJsonArray("pointers") ?: return null
        if (arr.size() == 0) return null
        val out = mutableListOf<GestureRunner.PointerPath>()
        for (i in 0 until arr.size()) {
            val el = arr[i].asJsonObject
            val id = el.get("id")?.takeIf { !it.isJsonNull }?.asString ?: "pointer$i"
            val waypointsArr = el.getAsJsonArray("waypoints") ?: return null
            val waypoints = mutableListOf<GestureRunner.Waypoint>()
            for (j in 0 until waypointsArr.size()) {
                val w = waypointsArr[j].asJsonObject
                val phase = w.get("phase")?.takeIf { !it.isJsonNull }?.asString ?: return null
                val x = w.get("x")?.takeIf { !it.isJsonNull }?.asFloat ?: return null
                val y = w.get("y")?.takeIf { !it.isJsonNull }?.asFloat ?: return null
                val offset = w.get("atOffsetSeconds")?.takeIf { !it.isJsonNull }?.asDouble
                    ?: return null
                val pressure = w.get("pressure")?.takeIf { !it.isJsonNull }?.asDouble
                waypoints.add(GestureRunner.Waypoint(phase, x, y, offset, pressure))
            }
            out.add(GestureRunner.PointerPath(id, waypoints))
        }
        return out
    }
}

class SwipeRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/swipe"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val result = services.gestures.swipe(
            fromX = body.get("fromX").asFloat,
            fromY = body.get("fromY").asFloat,
            toX = body.get("toX").asFloat,
            toY = body.get("toY").asFloat,
            durationMs = body.get("durationMs")?.takeIf { !it.isJsonNull }?.asLong ?: 300L,
        )
        return RouteResponse.ok(toJson(dispatchResultToMap(result)))
    }
}

class TypeKeyboardRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/type_keyboard"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val text = body.get("text").asString
        val perKeyDelayMs = body.get("perKeyDelayMs")?.takeIf { !it.isJsonNull }?.asLong ?: 80L
        val clearFirst = body.get("clearFirst")?.takeIf { !it.isJsonNull }?.asBoolean ?: true
        val result = services.keyboardTyper.typeViaKeyboard(text, perKeyDelayMs, clearFirst)
        return RouteResponse.ok(toJson(mapOf(
            "success" to result.success,
            "typed" to result.typed,
            "total" to result.total,
            "reason" to result.reason,
        )))
    }
}

class KeyRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/key"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.system.pressKey(body.get("key").asString)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}

class LaunchRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/launch"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.system.launchApp(body.get("packageName").asString)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}

class ForceStopRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/force_stop"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.system.forceStopApp(body.get("packageName").asString)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}
