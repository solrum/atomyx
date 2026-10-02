package dev.atomyx.agent.control.router

/**
 * The routes the control server registers. The server and the tests read
 * this one list, so a route added here is covered by every check that
 * walks the shipped routes.
 */
object ShippedRoutes {
    fun build(): List<Route> = listOf(
        PingRoute(),
        TreeRoute(),
        KeyboardRoute(),
        ScreenshotRoute(),
        CurrentActivityRoute(),
        AppsRoute(),
        TapCoordsRoute(),
        LongPressRoute(),
        ClearFocusedInputRoute(),
        HideKeyboardRoute(),
        SwipeRoute(),
        DispatchGestureRoute(),
        TypeKeyboardRoute(),
        KeyRoute(),
        LaunchRoute(),
        ForceStopRoute(),
    )
}
