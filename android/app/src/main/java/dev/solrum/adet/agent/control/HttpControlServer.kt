package dev.solrum.adet.agent.control

import android.util.Log
import dev.solrum.adet.agent.control.router.AppsRoute
import dev.solrum.adet.agent.control.router.ClearFocusedInputRoute
import dev.solrum.adet.agent.control.router.CurrentActivityRoute
import dev.solrum.adet.agent.control.router.ForceStopRoute
import dev.solrum.adet.agent.control.router.InputRoute
import dev.solrum.adet.agent.control.router.KeyRoute
import dev.solrum.adet.agent.control.router.KeyboardRoute
import dev.solrum.adet.agent.control.router.LaunchRoute
import dev.solrum.adet.agent.control.router.LongPressRoute
import dev.solrum.adet.agent.control.router.ResolveRoute
import dev.solrum.adet.agent.control.router.Route
import dev.solrum.adet.agent.control.router.RouteRequest
import dev.solrum.adet.agent.control.router.RouteResponse
import dev.solrum.adet.agent.control.router.Router
import dev.solrum.adet.agent.control.router.ScreenshotRoute
import dev.solrum.adet.agent.control.router.SwipeRoute
import dev.solrum.adet.agent.control.router.TapCoordsRoute
import dev.solrum.adet.agent.control.router.TapRoute
import dev.solrum.adet.agent.control.router.TreeRoute
import dev.solrum.adet.agent.control.router.TypeKeyboardRoute
import dev.solrum.adet.agent.service.AdetAccessibilityService
import fi.iki.elonen.NanoHTTPD

/**
 * Thin HTTP front-end. Single responsibility: parse incoming HTTP request,
 * dispatch to a Route via Router, serialize the RouteResponse back to NanoHTTPD.
 *
 * Lifecycle of services + cache invalidation lives in AdetServicesHolder.
 * Endpoint logic lives in router/CommonRoutes.kt.
 */
class HttpControlServer(
    accessibilityProvider: () -> AdetAccessibilityService?,
    port: Int = DEFAULT_PORT,
) : NanoHTTPD("127.0.0.1", port) {

    private val servicesHolder = AdetServicesHolder(accessibilityProvider)
    private val router = Router(buildRoutes())

    override fun serve(session: IHTTPSession): Response = try {
        // /health works without accessibility connected
        if (session.uri == "/health" && session.method == Method.GET) {
            val connected = servicesHolder.get() != null
            return newFixedLengthResponse(
                Response.Status.OK,
                "application/json",
                """{"ok":true,"accessibilityConnected":$connected}""",
            )
        }

        val services = servicesHolder.get() ?: return toResponse(
            RouteResponse.serviceUnavailable(
                "AccessibilityService not connected — enable SynapseAgent accessibility in Settings",
            ),
        )

        val request = RouteRequest(
            method = session.method,
            uri = session.uri,
            queryParams = session.parameters,
            headers = session.headers,
            body = readBody(session),
        )

        val response = router.dispatch(session.method, session.uri, request, services)
        toResponse(response)
    } catch (e: Exception) {
        Log.e(TAG, "control server error", e)
        toResponse(RouteResponse.internalError(e.message ?: "internal error"))
    }

    private fun buildRoutes(): List<Route> = listOf(
        TreeRoute(),
        KeyboardRoute(),
        ScreenshotRoute(),
        CurrentActivityRoute(),
        AppsRoute(),
        ResolveRoute(),
        TapRoute(),
        TapCoordsRoute(),
        LongPressRoute(),
        ClearFocusedInputRoute(),
        SwipeRoute(),
        InputRoute(),
        TypeKeyboardRoute(),
        KeyRoute(),
        LaunchRoute(),
        ForceStopRoute(),
    )

    private fun toResponse(r: RouteResponse): Response =
        newFixedLengthResponse(r.status, r.mimeType, r.body)

    /**
     * NanoHTTPD's parseBody decodes postData as ISO-8859-1 (mangles UTF-8).
     * Read raw bytes and decode as UTF-8 ourselves.
     */
    private fun readBody(session: IHTTPSession): String {
        val contentLength = session.headers["content-length"]?.toIntOrNull() ?: 0
        if (contentLength <= 0) return "{}"
        return try {
            val bytes = ByteArray(contentLength)
            var read = 0
            while (read < contentLength) {
                val r = session.inputStream.read(bytes, read, contentLength - read)
                if (r <= 0) break
                read += r
            }
            String(bytes, 0, read, Charsets.UTF_8)
        } catch (e: Exception) {
            Log.w(TAG, "readBody error", e)
            "{}"
        }
    }

    override fun stop() {
        servicesHolder.shutdown()
        super.stop()
    }

    companion object {
        private const val TAG = "HttpControlServer"
        const val DEFAULT_PORT = 8765
    }
}
