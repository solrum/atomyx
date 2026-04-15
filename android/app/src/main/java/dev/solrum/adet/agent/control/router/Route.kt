package dev.solrum.adet.agent.control.router

import dev.solrum.adet.agent.control.AdetServices
import fi.iki.elonen.NanoHTTPD

/**
 * Single HTTP endpoint. Implementations are pure: receive a request,
 * build a response. Lifecycle/cache concerns live in AdetServices.
 *
 * Adding a new endpoint:
 *   1. Create a new class implementing Route
 *   2. Register it in the Router constructor
 *   No existing files need to change.
 */
interface Route {
    val method: NanoHTTPD.Method
    val path: String
    fun handle(request: RouteRequest, services: AdetServices): RouteResponse
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
