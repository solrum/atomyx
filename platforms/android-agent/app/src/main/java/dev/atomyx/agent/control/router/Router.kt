package dev.atomyx.agent.control.router

import android.util.Log
import dev.atomyx.agent.control.AtomyxServices
import fi.iki.elonen.NanoHTTPD
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock

/**
 * Dispatches HTTP requests to the matching Route handler. Single-
 * pass O(n) lookup, which is fine while the route count stays well under
 * 100; a hash table would be premature optimization.
 *
 * Routes that declare [Route.mutates] run one at a time behind a single
 * lock, because the HTTP server handles each request on its own thread
 * and a multi-step action (typing reads the keyboard layout, then taps
 * keys) must not interleave with another gesture or key press.
 *
 * A mutating request waits for the lock at most [mutationWaitMs]. A
 * request still queued after that gets a retryable busy response and
 * never runs, so an action whose caller already gave up cannot fire
 * later behind a long gesture.
 *
 * Routes that do not mutate take no lock. Tree, keyboard, and
 * screenshot reads must stay answerable while a gesture is in flight,
 * since a held gesture can run for tens of seconds and a caller may
 * capture or inspect the screen during it. Tree reads are already
 * serialized against each other by the tree service's own guard.
 */
class Router(
    private val routes: List<Route>,
    private val mutationWaitMs: Long = MUTATION_LOCK_WAIT_MS,
) {

    private val mutationLock = ReentrantLock(true)

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
            if (match.mutates) {
                runMutating(match, request, services)
            } else {
                match.handle(request, services)
            }
        } catch (e: Exception) {
            Log.e(TAG, "route ${method} ${uri} threw", e)
            RouteResponse.internalError("Internal error.")
        }
    }

    private fun runMutating(route: Route, request: RouteRequest, services: AtomyxServices): RouteResponse {
        val acquired = try {
            mutationLock.tryLock(mutationWaitMs, TimeUnit.MILLISECONDS)
        } catch (e: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
        if (!acquired) {
            return RouteResponse.busy(BUSY_MESSAGE)
        }
        try {
            return route.handle(request, services)
        } finally {
            mutationLock.unlock()
        }
    }

    companion object {
        private const val TAG = "Router"

        /**
         * Longest a mutating request queues for the lock. It stays below the
         * host client's own request deadline, so the host receives the busy
         * response instead of timing out on a request that would run later.
         */
        const val MUTATION_LOCK_WAIT_MS = 5_000L

        const val BUSY_MESSAGE = "The agent is still running another action. Retry the call shortly."
    }
}
