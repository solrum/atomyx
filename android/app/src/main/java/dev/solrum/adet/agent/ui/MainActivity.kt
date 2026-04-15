package dev.solrum.adet.agent.ui

import android.os.Bundle
import android.view.Gravity
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import dev.solrum.adet.agent.control.AdetForegroundService
import dev.solrum.adet.agent.control.HttpControlServer
import dev.solrum.adet.agent.control.PermissionChecker

/**
 * Single-screen launcher for adet.
 *
 *   1. Shows accessibility + usage-stats permission status
 *   2. Deep-links to Settings to grant missing permissions
 *   3. Starts the AdetForegroundService when everything is ready
 *
 * No recording, no app picker, no server URL. adet is purely a control plane
 * for AI-driven exploratory testing — the host MCP client (apps/adet)
 * connects via `adb forward tcp:8765` once this app is running.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var statusText: TextView
    private lateinit var enableButton: Button

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        buildUi()
    }

    override fun onResume() {
        super.onResume()
        refreshStatus()
    }

    private fun buildUi() {
        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 64, 48, 48)
        }

        val title = TextView(this).apply {
            text = "adet"
            textSize = 32f
            setPadding(0, 0, 0, 8)
        }
        val subtitle = TextView(this).apply {
            text = "AI-driven exploratory testing"
            textSize = 14f
            alpha = 0.7f
            setPadding(0, 0, 0, 32)
        }

        statusText = TextView(this).apply {
            text = "checking permissions…"
            textSize = 14f
            setPadding(0, 0, 0, 32)
            typeface = android.graphics.Typeface.MONOSPACE
        }

        enableButton = Button(this).apply {
            text = "Enable adet control server"
            setOnClickListener { onEnableClicked() }
        }

        val openAccessibility = Button(this).apply {
            text = "Open Accessibility settings"
            setOnClickListener { PermissionChecker.deepLinkAccessibility(this@MainActivity) }
        }

        val openUsage = Button(this).apply {
            text = "Open Usage Access settings"
            setOnClickListener { PermissionChecker.deepLinkUsageStats(this@MainActivity) }
        }

        val helpText = TextView(this).apply {
            text = """

                Once enabled, connect from your host machine:

                  $ adb forward tcp:8765 tcp:8765
                  $ curl http://127.0.0.1:8765/health

                Or via the @synapse/adet MCP server.
            """.trimIndent()
            textSize = 12f
            alpha = 0.6f
            setPadding(0, 32, 0, 0)
            typeface = android.graphics.Typeface.MONOSPACE
        }

        layout.addView(title)
        layout.addView(subtitle)
        layout.addView(statusText)
        layout.addView(enableButton)
        layout.addView(openAccessibility)
        layout.addView(openUsage)
        layout.addView(helpText)

        setContentView(layout)
    }

    private fun refreshStatus() {
        if (!::statusText.isInitialized) return
        val s = PermissionChecker.status(this)
        statusText.text = buildString {
            append("permissions:\n")
            append(if (s.accessibilityEnabled) "  ✓ accessibility enabled\n" else "  ✗ accessibility DISABLED\n")
            append(if (s.accessibilityConnected) "  ✓ accessibility connected\n" else "  ✗ accessibility not connected\n")
            append(if (s.usageStatsGranted) "  ✓ usage stats granted\n" else "  ✗ usage stats NOT granted\n")
            append("\ncontrol server: 127.0.0.1:${HttpControlServer.DEFAULT_PORT}")
        }
    }

    private fun onEnableClicked() {
        val s = PermissionChecker.status(this)
        if (!s.accessibilityEnabled) {
            Toast.makeText(this, "Enable adet accessibility first", Toast.LENGTH_LONG).show()
            PermissionChecker.deepLinkAccessibility(this)
            return
        }
        if (!s.usageStatsGranted) {
            Toast.makeText(this, "Grant Usage Access for adet", Toast.LENGTH_LONG).show()
            PermissionChecker.deepLinkUsageStats(this)
            return
        }
        AdetForegroundService.start(this)
        Toast.makeText(this, "adet listening on :${HttpControlServer.DEFAULT_PORT}", Toast.LENGTH_SHORT).show()
        statusText.postDelayed({ refreshStatus() }, 500)
    }
}
