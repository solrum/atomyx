package dev.atomyx.demo

import io.flutter.embedding.android.FlutterActivity

class MainActivity : FlutterActivity() {
    // A "route" string extra on the launching intent selects the first
    // screen, so a launcher can start the app on any route without the
    // deep link flow.
    override fun getInitialRoute(): String? =
        intent?.getStringExtra(ROUTE_EXTRA) ?: super.getInitialRoute()

    private companion object {
        const val ROUTE_EXTRA = "route"
    }
}
