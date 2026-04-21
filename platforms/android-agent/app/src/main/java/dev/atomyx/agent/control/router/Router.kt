package dev.atomyx.agent.control.router

import android.util.Log
import dev.atomyx.agent.control.AtomyxServices
import fi.iki.elonen.NanoHTTPD

/**
 * Dispatches HTTP requests to the matching Route handler. Single-
 * pass O(n) lookup — fine while the route count stays well under
 * 100; a hash table would be premature optimization.
 */
class Router(private val routes: List<Route>) {

    fun dispatch(
        method: NanoHTTPD.Method,
        uri: String,
        request: RouteRequest,
        services: AtomyxServices,
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
