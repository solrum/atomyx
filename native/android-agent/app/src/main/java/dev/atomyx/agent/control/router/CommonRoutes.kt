package dev.atomyx.agent.control.router

import com.google.gson.Gson
import com.google.gson.JsonObject
import dev.atomyx.agent.control.AtomyxServices
import dev.atomyx.agent.control.SelectorResolver
import fi.iki.elonen.NanoHTTPD

private val gson = Gson()

private fun toJson(payload: Any) = gson.toJson(payload)

private fun parseBody(body: String): JsonObject =
    runCatching { gson.fromJson(body, JsonObject::class.java) }.getOrNull() ?: JsonObject()

private fun parseSelector(body: JsonObject): SelectorResolver.Selector {
    val sel = body.getAsJsonObject("selector") ?: body
    return SelectorResolver.Selector(
        resourceId = sel.get("resourceId")?.takeIf { !it.isJsonNull }?.asString,
        contentDesc = sel.get("contentDesc")?.takeIf { !it.isJsonNull }?.asString,
        text = sel.get("text")?.takeIf { !it.isJsonNull }?.asString,
        textContains = sel.get("textContains")?.takeIf { !it.isJsonNull }?.asString,
        hint = sel.get("hint")?.takeIf { !it.isJsonNull }?.asString,
        nth = sel.get("nth")?.takeIf { !it.isJsonNull }?.asInt ?: 0,
    )
}

// ────────────────────────────────────────────────────────────────────
// Read-only routes
// ────────────────────────────────────────────────────────────────────

class TreeRoute : Route {
    override val method = NanoHTTPD.Method.GET
    override val path = "/tree"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val format = request.queryParams["format"]?.firstOrNull() ?: "full"
        val payload = if (format == "compact") {
            mapOf("elements" to services.uiTree.dumpCompact())
        } else {
            services.uiTree.dumpTree()
        }
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
        val bytes = services.accessibility.takeScreenshotPng()
            ?: return RouteResponse.internalError("screenshot failed")
        val base64 = android.util.Base64.encodeToString(bytes, android.util.Base64.NO_WRAP)
        return RouteResponse.ok(toJson(mapOf("base64" to base64, "format" to "png")))
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
// Resolve / debug
// ────────────────────────────────────────────────────────────────────

class ResolveRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/resolve"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val selector = parseSelector(parseBody(request.body))
        val resolved = services.resolver.resolve(selector)
        if (resolved == null) return RouteResponse.ok(toJson(mapOf("found" to false)))
        try {
            val isInIme = try {
                resolved.node.window?.type ==
                    android.view.accessibility.AccessibilityWindowInfo.TYPE_INPUT_METHOD
            } catch (_: Exception) { false }
            return RouteResponse.ok(toJson(mapOf(
                "found" to true,
                "resolvedBy" to resolved.resolvedBy,
                "bounds" to mapOf(
                    "left" to resolved.bounds.left,
                    "top" to resolved.bounds.top,
                    "right" to resolved.bounds.right,
                    "bottom" to resolved.bounds.bottom,
                ),
                "resourceId" to resolved.node.viewIdResourceName,
                "text" to resolved.node.text?.toString(),
                "contentDesc" to resolved.node.contentDescription?.toString(),
                "className" to resolved.node.className?.toString(),
                "clickable" to resolved.node.isClickable,
                "enabled" to resolved.node.isEnabled,
                "isInIme" to isInIme,
            )))
        } finally {
            try { resolved.node.recycle() } catch (_: Exception) {}
        }
    }
}

// ────────────────────────────────────────────────────────────────────
// Action routes
// ────────────────────────────────────────────────────────────────────

class TapRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/tap"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val selector = parseSelector(parseBody(request.body))
        val result = services.gestures.tap(selector)
        return RouteResponse.ok(toJson(mapOf("ok" to result.success, "reason" to result.reason)))
    }
}

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

class InputRoute : Route {
    override val method = NanoHTTPD.Method.POST
    override val path = "/actions/input"
    override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
        val body = parseBody(request.body)
        val selector = parseSelector(body)
        val text = body.get("text").asString
        val result = services.gestures.inputText(selector, text)
        return RouteResponse.ok(toJson(mapOf("ok" to result.success, "reason" to result.reason)))
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
