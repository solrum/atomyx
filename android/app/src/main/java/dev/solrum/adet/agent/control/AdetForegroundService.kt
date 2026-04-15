package dev.solrum.adet.agent.control

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.util.Log
import dev.solrum.adet.agent.service.AdetAccessibilityService
import dev.solrum.adet.agent.ui.MainActivity

/**
 * Foreground service that owns the lifecycle of HttpControlServer.
 *
 * Decoupled from AdetAccessibilityService so that:
 *   1. Control server stays alive across accessibility toggles
 *   2. User can explicitly enable/disable adet without affecting recording
 *   3. Persistent notification gives clear visibility
 *
 * The control server still routes gesture actions through the accessibility service
 * instance (resolved via singleton), and returns 503 if accessibility is not connected.
 */
class AdetForegroundService : Service() {

    private var controlServer: HttpControlServer? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIFICATION_ID, buildNotification())
        startControlServer()
        return START_STICKY
    }

    override fun onDestroy() {
        try { controlServer?.stop() } catch (_: Exception) {}
        controlServer = null
        super.onDestroy()
    }

    private fun startControlServer() {
        if (controlServer != null) return
        try {
            controlServer = HttpControlServer(
                accessibilityProvider = { AdetAccessibilityService.instance },
            ).apply { start() }

            Log.i(TAG, "adet control server listening on 127.0.0.1:${HttpControlServer.DEFAULT_PORT}")
        } catch (e: Exception) {
            Log.e(TAG, "Failed to start adet control server", e)
        }
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "adet control server",
                NotificationManager.IMPORTANCE_LOW,
            ).apply {
                description = "Persistent service for AI-driven exploratory testing"
                setShowBadge(false)
            }
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
                .createNotificationChannel(channel)
        }
    }

    private fun buildNotification(): Notification {
        val openIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK
        }
        val pendingIntent = PendingIntent.getActivity(
            this, 0, openIntent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }

        return builder
            .setContentTitle("SynapseAgent — adet active")
            .setContentText("Control server on 127.0.0.1:${HttpControlServer.DEFAULT_PORT}")
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build()
    }

    companion object {
        private const val TAG = "AdetForeground"
        private const val CHANNEL_ID = "adet_control"
        private const val NOTIFICATION_ID = 4242

        fun start(context: Context) {
            val intent = Intent(context, AdetForegroundService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, AdetForegroundService::class.java))
        }
    }
}
