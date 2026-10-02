package dev.atomyx.agent.control.router

import dev.atomyx.agent.control.AtomyxServices
import fi.iki.elonen.NanoHTTPD

/**
 * Single HTTP endpoint. Implementations are pure: receive a request,
 * build a response. Lifecycle/cache concerns live in AtomyxServices.
 *
 * Adding a new endpoint:
 *   1. Create a new class implementing Route and declare `mutates`
 *   2. Register it in the Router constructor
 *   No existing files need to change.
 */
interface Route {
    val method: NanoHTTPD.Method
    val path: String

    /**
     * True when the route changes device state or drives the
     * accessibility service (gestures, typing, key presses, app
     * lifecycle). The router runs mutating routes one at a time. A
     * route that only reads state, or whose work must stay possible
     * while a mutating route is in flight, declares false.
     */
    val mutates: Boolean
    fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse
}

data class RouteRequest(
    val method: NanoHTTPD.Method,
    val uri: String,
    val queryParams: Map<String, List<String>>,
    val headers: Map<String, String>,
    val body: String,
)

data class RouteResponse(
    val status: NanoHTTPD.Response.Status,
    val mimeType: String,
    val body: String,
) {
    companion object {
        fun ok(json: String) = RouteResponse(NanoHTTPD.Response.Status.OK, "application/json", json)
        fun notFound(message: String) = RouteResponse(
            NanoHTTPD.Response.Status.NOT_FOUND,
            "application/json",
            """{"error":"$message"}""",
        )
        fun badRequest(message: String) = RouteResponse(
            NanoHTTPD.Response.Status.BAD_REQUEST,
            "application/json",
            """{"error":"${message.replace("\"", "\\\"")}"}""",
        )
        fun busy(message: String) = RouteResponse(
            NanoHTTPD.Response.Status.TOO_MANY_REQUESTS,
            "application/json",
            """{"error":"${message.replace("\"", "\\\"")}","retryable":true}""",
        )
        fun serviceUnavailable(message: String) = RouteResponse(
            NanoHTTPD.Response.Status.SERVICE_UNAVAILABLE,
            "application/json",
            """{"error":"${message.replace("\"", "\\\"")}"}""",
        )
        fun internalError(message: String) = RouteResponse(
            NanoHTTPD.Response.Status.INTERNAL_ERROR,
            "application/json",
            """{"error":"${message.replace("\"", "\\\"")}"}""",
        )
    }
}
