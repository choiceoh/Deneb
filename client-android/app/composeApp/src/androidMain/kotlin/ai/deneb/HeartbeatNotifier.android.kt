package ai.deneb

import ai.deneb.shared.R
import ai.deneb.tools.AI_NOTIFICATION_CHANNEL_ID
import ai.deneb.tools.canPostNotifications
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.notification_channel_description
import deneb.composeapp.generated.resources.notification_channel_name
import kotlinx.coroutines.runBlocking
import org.jetbrains.compose.resources.getString
import org.koin.java.KoinJavaComponent.inject

/**
 * Intent extra read by MainActivity when the user taps a heartbeat notification. The
 * receiver forwards the signal to `DataRepository.requestOpenHeartbeat()` so the
 * ChatViewModel observer can load the heartbeat conversation.
 */
const val EXTRA_OPEN_HEARTBEAT = "ai.deneb.OPEN_HEARTBEAT"

/**
 * Intent extra for a proactive-report notification: MainActivity forwards it to
 * `DataRepository.requestOpenWorkTopic()` so the 업무 (General) topic — where the
 * report was mirrored — opens, instead of the heartbeat conversation.
 */
const val EXTRA_OPEN_WORK_TOPIC = "ai.deneb.OPEN_WORK_TOPIC"

/** Intent extra carrying a specific work-feed item to expand after navigation. */
const val EXTRA_OPEN_WORK_FEED_ITEM_ID = "ai.deneb.OPEN_WORK_FEED_ITEM_ID"

/**
 * Fixed ID so a new heartbeat report replaces any earlier unread one in the tray
 * instead of piling up. Android notification IDs are app-wide, so keep this
 * distinct from foreground-service and proactive-report IDs.
 */
private const val HEARTBEAT_NOTIFICATION_ID = 9003

/** Separate tray ID so a proactive report doesn't replace an unread heartbeat. */
const val PROACTIVE_NOTIFICATION_ID = 9004

actual fun sendHeartbeatNotification(title: String, body: String) {
    postNotification(title, body, EXTRA_OPEN_HEARTBEAT, HEARTBEAT_NOTIFICATION_ID)
}

/**
 * The work-feed item currently occupying the proactive tray slot. The slot is a
 * single fixed id, so remembering its occupant is what lets an in-app settle
 * cancel the right notification and only that one.
 */
private val proactiveTrayItemId = java.util.concurrent.atomic.AtomicReference<String>("")

actual fun cancelWorkFeedNotification(itemId: String) {
    val target = itemId.trim()
    if (target.isEmpty()) return
    if (!proactiveTrayItemId.compareAndSet(target, "")) return
    val context: Context by inject(Context::class.java)
    val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
    manager.cancel(PROACTIVE_NOTIFICATION_ID)
}

actual fun sendProactiveReportNotification(
    title: String,
    body: String,
    kind: String,
    ref: String,
    approveActionId: String?,
    rejectActionId: String?,
) {
    val itemId = workFeedItemId(kind, ref)
    proactiveTrayItemId.set(itemId?.trim().orEmpty())
    if (itemId != null) {
        postNotification(
            title = title,
            body = body,
            deepLinkExtra = EXTRA_OPEN_WORK_FEED_ITEM_ID,
            notificationId = PROACTIVE_NOTIFICATION_ID,
            deepLinkValue = itemId,
            approveActionId = approveActionId?.takeIf { itemId.isNotBlank() },
            rejectActionId = rejectActionId?.takeIf { itemId.isNotBlank() },
        )
    } else {
        postNotification(title, body, EXTRA_OPEN_WORK_TOPIC, PROACTIVE_NOTIFICATION_ID)
    }
}

/**
 * Posts a tray notification whose tap deep-links via [deepLinkExtra] (one of the
 * EXTRA_OPEN_* keys). [deepLinkValue] carries an opaque target ID when present;
 * legacy boolean extras remain unchanged when it is null. [notificationId] keeps
 * heartbeat vs proactive reports in separate tray slots. [approveActionId]/
 * [rejectActionId] add Trust Inbox tray buttons that settle the card's approval
 * action via [WorkFeedActionReceiver] without opening the app.
 */
private fun postNotification(
    title: String,
    body: String,
    deepLinkExtra: String,
    notificationId: Int,
    deepLinkValue: String? = null,
    approveActionId: String? = null,
    rejectActionId: String? = null,
) {
    val context: Context by inject(Context::class.java)
    val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    ensureAiNotificationChannel(notificationManager)
    if (!canPostNotifications(context, AI_NOTIFICATION_CHANNEL_ID)) return

    val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)?.apply {
        flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
        if (deepLinkValue == null) {
            putExtra(deepLinkExtra, true)
        } else {
            putExtra(deepLinkExtra, deepLinkValue)
        }
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
        .setContentText(body)
        .setStyle(NotificationCompat.BigTextStyle().bigText(body))
        .setPriority(NotificationCompat.PRIORITY_DEFAULT)
        .setAutoCancel(true)

    pendingIntent?.let { notificationBuilder.setContentIntent(it) }

    // Trust Inbox approve/reject tray buttons. Distinct request codes keep the
    // two PendingIntents from colliding with the content tap or each other.
    val actionItemId = deepLinkValue?.takeIf { it.isNotBlank() }
    if (actionItemId != null) {
        approveActionId?.let { id ->
            notificationBuilder.addAction(
                0,
                "승인",
                workFeedActionPendingIntent(context, notificationId, 1, actionItemId, id),
            )
        }
        rejectActionId?.let { id ->
            notificationBuilder.addAction(
                0,
                "거절",
                workFeedActionPendingIntent(context, notificationId, 2, actionItemId, id),
            )
        }
    }

    val notification = notificationBuilder.build()

    runCatching { notificationManager.notify(notificationId, notification) }
}

/**
 * Lazily creates the shared AI notification channel. Public because the
 * androidApp module's FCM messaging path (FcmService/DenebReplyReceiver) posts
 * on a cold app process where NotificationHelper may never have run.
 */
fun ensureAiNotificationChannel(manager: NotificationManager) {
    if (manager.getNotificationChannel(AI_NOTIFICATION_CHANNEL_ID) != null) return
    val name = runBlocking { getString(Res.string.notification_channel_name) }
    val description = runBlocking { getString(Res.string.notification_channel_description) }
    manager.createNotificationChannel(
        NotificationChannel(AI_NOTIFICATION_CHANNEL_ID, name, NotificationManager.IMPORTANCE_DEFAULT).apply {
            this.description = description
        },
    )
}

/** Broadcast PendingIntent routing one Trust Inbox tray button tap. */
private fun workFeedActionPendingIntent(
    context: Context,
    notificationId: Int,
    slot: Int,
    itemId: String,
    actionId: String,
): PendingIntent = PendingIntent.getBroadcast(
    context,
    notificationId * 10 + slot,
    Intent(context, WorkFeedActionReceiver::class.java).apply {
        action = WorkFeedActionReceiver.ACTION_RUN_WORK_FEED_ACTION
        putExtra(WorkFeedActionReceiver.EXTRA_WORK_FEED_ITEM_ID, itemId)
        putExtra(WorkFeedActionReceiver.EXTRA_WORK_FEED_ACTION_ID, actionId)
    },
    PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
)
