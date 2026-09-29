package dev.atomyx.agent.control.router

import android.util.Log
import dev.atomyx.agent.control.AtomyxServices
import fi.iki.elonen.NanoHTTPD

/**
 * Dispatches HTTP requests to the matching Route handler. Single-
 * pass O(n) lookup, which is fine while the route count stays well under
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
        if (match == null) {
            Log.w(TAG, "no route for $method $uri")
            return RouteResponse.notFound("Unsupported command.")
        }
        return try {
            match.handle(request, services)
        } catch (e: Exception) {
            Log.e(TAG, "route ${method} ${uri} threw", e)
            RouteResponse.internalError("Internal error.")
        }
    }

    companion object {
        private const val TAG = "Router"
    }
}
