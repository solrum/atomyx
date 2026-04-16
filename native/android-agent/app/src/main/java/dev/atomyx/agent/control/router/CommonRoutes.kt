package dev.atomyx.agent.control.router

import com.google.gson.Gson
import com.google.gson.JsonObject
import dev.atomyx.agent.control.AtomyxServices
import fi.iki.elonen.NanoHTTPD

private val gson = Gson()

private fun toJson(payload: Any) = gson.toJson(payload)

private fun parseBody(body: String): JsonObject =
    runCatching { gson.fromJson(body, JsonObject::class.java) }.getOrNull() ?: JsonObject()

// ────────────────────────────────────────────────────────────────────
// Read-only routes
// ────────────────────────────────────────────────────────────────────

class TreeRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/tree"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        // Always returns the hierarchical RawElementDto tree. The
        // `format=compact` flat-list variant was removed when the
        // legacy `src/adapters/agent-direct.adapter.ts` caller was
        // retired — `dumpCompact` stays on `UiTreeService` because
        // GestureDispatcher's on-screen-key fallback still uses it
        // internally, but it is no longer exposed on the wire.
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

class TapCoordsRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/tap_coords"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.gestures.tapAt(body.get("x").asFloat, body.get("y").asFloat)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}

class LongPressRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/long_press"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val duration = body.get("durationMs")?.takeIf { !it.isJsonNull }?.asLong ?: 800L
        services.gestures.longPressAt(
            body.get("x").asFloat,
            body.get("y").asFloat,
            duration,
        )
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}

class ClearFocusedInputRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/clear_focused_input"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val result = services.gestures.clearFocusedInput()
        return RouteResponse.ok(toJson(mapOf("ok" to result.success, "reason" to result.reason)))
    }
}

class SwipeRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/swipe"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.gestures.swipe(
            fromX = body.get("fromX").asFloat,
            fromY = body.get("fromY").asFloat,
            toX = body.get("toX").asFloat,
            toY = body.get("toY").asFloat,
            durationMs = body.get("durationMs")?.takeIf { !it.isJsonNull }?.asLong ?: 300L,
        )
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
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
        val result = services.gestures.typeViaKeyboard(text, perKeyDelayMs, clearFirst)
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
        services.gestures.pressKey(body.get("key").asString)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}

class LaunchRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/launch"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.gestures.launchApp(body.get("packageName").asString)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}

class ForceStopRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/force_stop"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        services.gestures.forceStopApp(body.get("packageName").asString)
        return RouteResponse.ok(toJson(mapOf("ok" to true)))
    }
}
