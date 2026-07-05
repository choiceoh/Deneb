package ai.deneb.tools

import android.app.NotificationManager
import android.content.Context
import android.os.Build
import androidx.core.app.NotificationManagerCompat

// Public (not internal): the androidApp module's FCM/Auto messaging path posts
// to the same channel so all Deneb notifications share one user-facing switch.
const val AI_NOTIFICATION_CHANNEL_ID = "kai_ai_notifications"

fun canPostNotifications(
    context: Context,
    channelId: String? = null,
): Boolean {
    if (!NotificationManagerCompat.from(context).areNotificationsEnabled()) {
        return false
    }

    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && channelId != null) {
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = manager.getNotificationChannel(channelId)
        if (channel?.importance == NotificationManager.IMPORTANCE_NONE) {
            return false
        }
    }

    return true
}
