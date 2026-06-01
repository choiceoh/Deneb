package com.inspiredandroid.kai

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import com.inspiredandroid.kai.shared.R
import com.inspiredandroid.kai.tools.AI_NOTIFICATION_CHANNEL_ID
import com.inspiredandroid.kai.tools.canPostNotifications
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

/**
 * Intent extra for a proactive-report notification: MainActivity forwards it to
 * `DataRepository.requestOpenWorkTopic()` so the 업무 (General) topic — where the
 * report was mirrored — opens, instead of the heartbeat conversation.
 */
const val EXTRA_OPEN_WORK_TOPIC = "com.inspiredandroid.kai.OPEN_WORK_TOPIC"

/**
 * Fixed ID so a new heartbeat report replaces any earlier unread one in the tray
 * instead of piling up. The app only ever has one pending heartbeat conversation.
 */
private const val HEARTBEAT_NOTIFICATION_ID = 9002

/** Separate tray ID so a proactive report doesn't replace an unread heartbeat. */
private const val PROACTIVE_NOTIFICATION_ID = 9003

actual fun sendHeartbeatNotification(title: String, body: String) {
    postNotification(title, body, EXTRA_OPEN_HEARTBEAT, HEARTBEAT_NOTIFICATION_ID)
}

actual fun sendProactiveReportNotification(title: String, body: String) {
    postNotification(title, body, EXTRA_OPEN_WORK_TOPIC, PROACTIVE_NOTIFICATION_ID)
}

/**
 * Posts a tray notification whose tap deep-links via [deepLinkExtra] (one of the
 * EXTRA_OPEN_* keys). [notificationId] keeps heartbeat vs proactive reports in
 * separate tray slots so one doesn't replace the other.
 */
private fun postNotification(title: String, body: String, deepLinkExtra: String, notificationId: Int) {
    val context: Context by inject(Context::class.java)
    val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    ensureChannel(notificationManager)
    if (!canPostNotifications(context, AI_NOTIFICATION_CHANNEL_ID)) return

    val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)?.apply {
        flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
        putExtra(deepLinkExtra, true)
    }
    val pendingIntent = PendingIntent.getActivity(
        context,
        notificationId,
        intent,
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )

    val notification = NotificationCompat.Builder(context, AI_NOTIFICATION_CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_notification)
        .setContentTitle(title)
        .setContentText(body)
        .setStyle(NotificationCompat.BigTextStyle().bigText(body))
        .setPriority(NotificationCompat.PRIORITY_DEFAULT)
        .setContentIntent(pendingIntent)
        .setAutoCancel(true)
        .build()

    notificationManager.notify(notificationId, notification)
}

private fun ensureChannel(manager: NotificationManager) {
    if (manager.getNotificationChannel(AI_NOTIFICATION_CHANNEL_ID) != null) return
    val name = runBlocking { getString(Res.string.notification_channel_name) }
    val description = runBlocking { getString(Res.string.notification_channel_description) }
    manager.createNotificationChannel(
        NotificationChannel(AI_NOTIFICATION_CHANNEL_ID, name, NotificationManager.IMPORTANCE_DEFAULT).apply {
            this.description = description
        },
    )
}
