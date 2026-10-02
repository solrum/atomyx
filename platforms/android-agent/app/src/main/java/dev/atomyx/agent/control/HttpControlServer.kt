package dev.atomyx.agent.control

import android.util.Log
import dev.atomyx.agent.control.router.RouteRequest
import dev.atomyx.agent.control.router.RouteResponse
import dev.atomyx.agent.control.router.Router
import dev.atomyx.agent.control.router.ShippedRoutes
import dev.atomyx.agent.service.AtomyxAccessibilityService
import fi.iki.elonen.NanoHTTPD

/**
 * Thin HTTP front-end. Single responsibility: parse incoming HTTP request,
 * dispatch to a Route via Router, serialize the RouteResponse back to NanoHTTPD.
 *
 * Lifecycle of services + cache invalidation lives in AtomyxServicesHolder.
 * Endpoint logic lives in router/CommonRoutes.kt.
 */
class HttpControlServer(
    accessibilityProvider: () -> AtomyxAccessibilityService?,
    port: Int = DEFAULT_PORT,
) : NanoHTTPD("127.0.0.1", port) {

    private val servicesHolder = AtomyxServicesHolder(accessibilityProvider)
    private val router = Router(ShippedRoutes.build())

    override fun serve(session: IHTTPSession): Response = try {
        // /health works without accessibility connected
        if (session.uri == "/health" && session.method == Method.GET) {
            val instance = servicesHolder.get()?.accessibility
            val decision = HealthEvaluator.evaluate(
                HealthFacts(
                    serviceConnected = instance != null,
                    registeredWithSystem = instance?.isRegisteredWithSystem() ?: false,
                    canReadScreen = instance?.canReadScreen() ?: false,
                ),
            )
            val status = if (decision.ok) Response.Status.OK else Response.Status.SERVICE_UNAVAILABLE
            val body = buildString {
                append("""{"ok":${decision.ok},"accessibilityConnected":${decision.accessibilityConnected}""")
                decision.reason?.let { append(""","reason":"$it"""") }
                append("}")
            }
            return newFixedLengthResponse(status, "application/json", body)
        }

        val services = servicesHolder.get() ?: return toResponse(
            RouteResponse.serviceUnavailable(
                "AccessibilityService not connected. Enable Atomyx accessibility in Settings",
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
        toResponse(RouteResponse.internalError("Internal error."))
    }

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
