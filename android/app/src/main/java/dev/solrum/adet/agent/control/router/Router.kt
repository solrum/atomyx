package dev.solrum.adet.agent.control.router

import android.util.Log
import dev.solrum.adet.agent.control.AdetServices
import fi.iki.elonen.NanoHTTPD

/**
 * Routes HTTP requests to a Route handler. Single-pass O(n) lookup is fine
 * for small route counts (<100). Replaces the giant when() switch in
 * HttpControlServer.serve().
 */
class Router(private val routes: List<Route>) {

    fun dispatch(
        method: NanoHTTPD.Method,
        uri: String,
        request: RouteRequest,
        services: AdetServices,
    ): RouteResponse {
        val match = routes.firstOrNull { it.method == method && it.path == uri }
            ?: return RouteResponse.notFound("Unknown $method $uri")
        return try {
            match.handle(request, services)
        } catch (e: Exception) {
            Log.e(TAG, "route ${method} ${uri} threw", e)
            RouteResponse.internalError(e.message ?: "internal error")
        }
    }

    companion object {
        private const val TAG = "Router"
    }
}
