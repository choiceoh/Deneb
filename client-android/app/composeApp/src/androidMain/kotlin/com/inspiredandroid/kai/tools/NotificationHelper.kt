package com.inspiredandroid.kai.tools

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import com.inspiredandroid.kai.shared.R
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.notification_channel_description
import kai.composeapp.generated.resources.notification_channel_name
import kotlinx.coroutines.runBlocking
import org.jetbrains.compose.resources.getString
import java.util.concurrent.atomic.AtomicInteger

class NotificationHelper(
    private val context: Context,
    private val permissionController: NotificationPermissionController,
) {
    private val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
    private val notificationIdCounter = AtomicInteger(0)

    init {
        createNotificationChannel()
    }

    private fun createNotificationChannel() {
        val channelName = runBlocking { getString(Res.string.notification_channel_name) }
        val channelDescription = runBlocking { getString(Res.string.notification_channel_description) }
        val channel = NotificationChannel(
            AI_NOTIFICATION_CHANNEL_ID,
            channelName,
            NotificationManager.IMPORTANCE_DEFAULT,
        ).apply {
            description = channelDescription
        }
        notificationManager.createNotificationChannel(channel)
    }

    suspend fun sendNotification(
        title: String,
        message: String,
    ): NotificationResult {
        // Check and request permission if needed
        if (!permissionController.hasPermission()) {
            val granted = permissionController.requestPermission()
            if (!granted) {
                return NotificationResult.Error("Notification permission denied")
            }
        }

        if (!canPostNotifications(context, AI_NOTIFICATION_CHANNEL_ID)) {
            return NotificationResult.Error("Notifications are disabled for this app or notification channel")
        }

        return try {
            val notificationId = notificationIdCounter.incrementAndGet()

            val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)?.apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            }
            val pendingIntent = intent?.let {
                PendingIntent.getActivity(
                    context,
                    notificationId,
                    it,
                    PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
                )
            }

            val notificationBuilder = NotificationCompat.Builder(context, AI_NOTIFICATION_CHANNEL_ID)
                .setSmallIcon(R.drawable.ic_notification)
                .setContentTitle(title)
                .setContentText(message)
                .setStyle(NotificationCompat.BigTextStyle().bigText(message))
                .setPriority(NotificationCompat.PRIORITY_DEFAULT)
                .setAutoCancel(true)

            pendingIntent?.let { notificationBuilder.setContentIntent(it) }

            val notification = notificationBuilder.build()

            notificationManager.notify(notificationId, notification)

            NotificationResult.Success(notificationId, message)
        } catch (e: Exception) {
            NotificationResult.Error("Failed to send notification: ${e.message}")
        }
    }
}

sealed class NotificationResult {
    data class Success(val notificationId: Int, val message: String) : NotificationResult()
    data class Error(val message: String) : NotificationResult()
}
