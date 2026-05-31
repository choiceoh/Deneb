package com.inspiredandroid.kai

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.inspiredandroid.kai.shared.R
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.notification_channel_description
import kai.composeapp.generated.resources.notification_channel_name
import kotlinx.coroutines.runBlocking
import org.jetbrains.compose.resources.getString
import org.koin.java.KoinJavaComponent.inject

/**
 * Intent extra read by MainActivity when the user taps a heartbeat notification. The
 * receiver forwards the signal to `DataRepository.requestOpenHeartbeat()` so the
 * ChatViewModel observer can load the heartbeat conversation.
 */
const val EXTRA_OPEN_HEARTBEAT = "com.inspiredandroid.kai.OPEN_HEARTBEAT"

/** Shared with the AI `send_notification` tool — ensures the channel is created once. */
private const val CHANNEL_ID = "kai_ai_notifications"

/**
 * Fixed ID so a new heartbeat report replaces any earlier unread one in the tray
 * instead of piling up. Keep this distinct from foreground-service notification IDs
 * because Android notification IDs are app-wide, not channel-scoped.
 */
private const val HEARTBEAT_NOTIFICATION_ID = 9003

actual fun sendHeartbeatNotification(title: String, body: String) {
    val context: Context by inject(Context::class.java)
    if (!hasNotificationPermission(context)) return

    val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    ensureChannel(notificationManager)

    val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)?.apply {
        flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
        putExtra(EXTRA_OPEN_HEARTBEAT, true)
    }
    val pendingIntent = intent?.let {
        PendingIntent.getActivity(
            context,
            HEARTBEAT_NOTIFICATION_ID,
            it,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    val notificationBuilder = NotificationCompat.Builder(context, CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_notification)
        .setContentTitle(title)
        .setContentText(body)
        .setStyle(NotificationCompat.BigTextStyle().bigText(body))
        .setPriority(NotificationCompat.PRIORITY_DEFAULT)
        .setAutoCancel(true)

    pendingIntent?.let { notificationBuilder.setContentIntent(it) }

    val notification = notificationBuilder.build()

    runCatching { notificationManager.notify(HEARTBEAT_NOTIFICATION_ID, notification) }
}

private fun hasNotificationPermission(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(
            context,
            Manifest.permission.POST_NOTIFICATIONS,
        ) == PackageManager.PERMISSION_GRANTED

private fun ensureChannel(manager: NotificationManager) {
    if (manager.getNotificationChannel(CHANNEL_ID) != null) return
    val name = runBlocking { getString(Res.string.notification_channel_name) }
    val description = runBlocking { getString(Res.string.notification_channel_description) }
    manager.createNotificationChannel(
        NotificationChannel(CHANNEL_ID, name, NotificationManager.IMPORTANCE_DEFAULT).apply {
            this.description = description
        },
    )
}
