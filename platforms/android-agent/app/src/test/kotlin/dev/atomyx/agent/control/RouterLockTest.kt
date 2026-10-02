package dev.atomyx.agent.control

import dev.atomyx.agent.control.router.Route
import dev.atomyx.agent.control.router.RouteRequest
import dev.atomyx.agent.control.router.RouteResponse
import dev.atomyx.agent.control.router.Router
import dev.atomyx.agent.control.router.ShippedRoutes
import fi.iki.elonen.NanoHTTPD
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the router's concurrency contract. The HTTP server runs each
 * request on its own thread, so two action requests can reach the
 * accessibility service at once. Every ordering check uses latches and
 * thread-state observation instead of timing, so a failure means the
 * contract broke and never that a machine was slow.
 */
class RouterLockTest {

    private val events = ConcurrentLinkedQueue<String>()

    private inner class LatchedRoute(
        override val path: String,
        override val mutates: Boolean,
        private val holdUntil: CountDownLatch? = null,
    ) : Route {
        override val method = NanoHTTPD.Method.POST
        val entered = CountDownLatch(1)

        override fun handle(request: RouteRequest, services: AtomyxServices): RouteResponse {
            events.add("enter $path")
            entered.countDown()
            holdUntil?.await(AWAIT_SECONDS, TimeUnit.SECONDS)
            events.add("exit $path")
            return RouteResponse.ok("{}")
        }
    }

    private fun dispatchOnThread(router: Router, path: String): Thread =
        Thread {
            router.dispatch(NanoHTTPD.Method.POST, path, request(path), services)
        }.apply { isDaemon = true; start() }

    private fun request(path: String) = RouteRequest(
        method = NanoHTTPD.Method.POST,
        uri = path,
        queryParams = emptyMap(),
        headers = emptyMap(),
        body = "{}",
    )

    /** Spins until [thread] has either finished or parked waiting for a lock, bounded or not. */
    private fun awaitSettled(thread: Thread) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(AWAIT_SECONDS)
        while (thread.isAlive && thread.state != Thread.State.WAITING &&
            thread.state != Thread.State.TIMED_WAITING &&
            thread.state != Thread.State.BLOCKED
        ) {
            check(System.nanoTime() < deadline) { "thread never settled: ${thread.state}" }
            Thread.onSpinWait()
        }
    }

    @Test
    fun mutatingRoutesNeverInterleave() {
        val release = CountDownLatch(1)
        val first = LatchedRoute("/actions/first", mutates = true, holdUntil = release)
        val second = LatchedRoute("/actions/second", mutates = true)
        val router = Router(listOf(first, second))

        val firstThread = dispatchOnThread(router, first.path)
        assertTrue(first.entered.await(AWAIT_SECONDS, TimeUnit.SECONDS))
        val secondThread = dispatchOnThread(router, second.path)
        awaitSettled(secondThread)

        assertEquals(
            "second action started while the first still held the device",
            listOf("enter /actions/first"),
            events.toList(),
        )

        release.countDown()
        firstThread.join(JOIN_MS)
        secondThread.join(JOIN_MS)
        assertEquals(
            listOf(
                "enter /actions/first", "exit /actions/first",
                "enter /actions/second", "exit /actions/second",
            ),
            events.toList(),
        )
    }

    @Test
    fun screenshotCompletesWhileAGestureHoldsTheLock() {
        val release = CountDownLatch(1)
        val gesture = LatchedRoute("/actions/dispatch_gesture", mutates = true, holdUntil = release)
        val screenshot = LatchedRoute("/screenshot", mutates = false)
        val router = Router(listOf(gesture, screenshot))

        val gestureThread = dispatchOnThread(router, gesture.path)
        assertTrue(gesture.entered.await(AWAIT_SECONDS, TimeUnit.SECONDS))
        val screenshotThread = dispatchOnThread(router, screenshot.path)

        screenshotThread.join(JOIN_MS)
        assertFalse("capture was blocked behind the gesture", screenshotThread.isAlive)
        assertTrue(events.contains("exit /screenshot"))
        assertFalse(events.contains("exit /actions/dispatch_gesture"))

        release.countDown()
        gestureThread.join(JOIN_MS)
    }

    @Test
    fun treeReadCompletesWhileAGestureHoldsTheLock() {
        val release = CountDownLatch(1)
        val gesture = LatchedRoute("/actions/swipe", mutates = true, holdUntil = release)
        val tree = LatchedRoute("/tree", mutates = false)
        val router = Router(listOf(gesture, tree))

        val gestureThread = dispatchOnThread(router, gesture.path)
        assertTrue(gesture.entered.await(AWAIT_SECONDS, TimeUnit.SECONDS))
        val treeThread = dispatchOnThread(router, tree.path)

        treeThread.join(JOIN_MS)
        assertFalse("tree read was blocked behind the gesture", treeThread.isAlive)
        assertTrue(events.contains("exit /tree"))

        release.countDown()
        gestureThread.join(JOIN_MS)
    }

    @Test
    fun aMutatingRequestStillQueuedAfterTheBoundedWaitGetsABusyResponseAndNeverRuns() {
        val release = CountDownLatch(1)
        val first = LatchedRoute("/actions/first", mutates = true, holdUntil = release)
        val second = LatchedRoute("/actions/second", mutates = true)
        val router = Router(listOf(first, second), mutationWaitMs = 1)

        val firstThread = dispatchOnThread(router, first.path)
        assertTrue(first.entered.await(AWAIT_SECONDS, TimeUnit.SECONDS))
        val response = router.dispatch(NanoHTTPD.Method.POST, second.path, request(second.path), services)

        assertEquals(NanoHTTPD.Response.Status.TOO_MANY_REQUESTS, response.status)
        assertTrue(response.body, response.body.contains("\"retryable\":true"))
        assertTrue(response.body, response.body.contains(Router.BUSY_MESSAGE))

        release.countDown()
        firstThread.join(JOIN_MS)
        assertEquals(
            "the queued action ran after its caller was told to retry",
            listOf("enter /actions/first", "exit /actions/first"),
            events.toList(),
        )

        val retry = router.dispatch(NanoHTTPD.Method.POST, second.path, request(second.path), services)
        assertEquals(NanoHTTPD.Response.Status.OK, retry.status)
    }

    @Test
    fun everyRouteTheServerRegistersDeclaresItsMutability() {
        val readPaths = setOf("/ping", "/tree", "/keyboard", "/screenshot", "/current-activity", "/apps")
        val shipped = ShippedRoutes.build()

        assertTrue("no shipped routes", shipped.isNotEmpty())
        shipped.forEach { route ->
            if (route.path in readPaths) {
                assertFalse("${route.path} must stay outside the lock", route.mutates)
            } else {
                assertTrue(
                    "${route.path} must run behind the lock, or be added to the read paths on purpose",
                    route.mutates,
                )
            }
        }
        assertTrue(
            "a listed read path is not shipped: ${readPaths - shipped.map { it.path }.toSet()}",
            shipped.map { it.path }.containsAll(readPaths),
        )
    }

    private companion object {
        const val AWAIT_SECONDS = 10L
        const val JOIN_MS = 10_000L

        /**
         * Routes in these tests never read their services argument, and a
         * real instance needs a live accessibility service, so the
         * instance is allocated without running its constructor.
         */
        val services: AtomyxServices = run {
            val unsafeClass = Class.forName("sun.misc.Unsafe")
            val field = unsafeClass.getDeclaredField("theUnsafe")
            field.isAccessible = true
            val allocate = unsafeClass.getMethod("allocateInstance", Class::class.java)
            allocate.invoke(field.get(null), AtomyxServices::class.java) as AtomyxServices
        }
    }
}
