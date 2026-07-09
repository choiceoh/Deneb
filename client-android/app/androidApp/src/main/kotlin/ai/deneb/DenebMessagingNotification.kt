package ai.deneb

import ai.deneb.shared.R
import ai.deneb.tools.AI_NOTIFICATION_CHANNEL_ID
import ai.deneb.tools.canPostNotifications
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.Person
import androidx.core.app.RemoteInput

/**
 * Builds and updates the proactive-report notification as a MessagingStyle
 * conversation so Android Auto can read it aloud and take a voice reply.
 *
 * Android Auto only surfaces notifications that carry this exact shape:
 * MessagingStyle + a reply Action with a RemoteInput (SemanticAction REPLY,
 * showsUserInterface=false, allowGeneratedReplies=true) + a mark-as-read Action
 * (SemanticAction MARK_AS_READ, showsUserInterface=false). On the phone tray the
 * same notification keeps the existing behavior: tap deep-links into the 업무
 * topic (EXTRA_OPEN_WORK_TOPIC), and the inline reply field posts back to the
 * gateway via [DenebReplyReceiver].
 */
object DenebMessagingNotification {
    /**
     * Fixed tray slot for the Deneb conversation. Distinct from the heartbeat
     * (9003) and legacy proactive (9004) IDs so it never replaces those.
     */
    const val NOTIFICATION_ID = 9005

    const val ACTION_REPLY = "ai.deneb.action.NOTIFICATION_REPLY"
    const val ACTION_MARK_AS_READ = "ai.deneb.action.NOTIFICATION_MARK_AS_READ"

    /** RemoteInput result key carrying the (voice) reply text. */
    const val KEY_REPLY_TEXT = "ai.deneb.extra.REPLY_TEXT"

    // Stable request codes so PendingIntents update in place instead of piling up.
    private const val REQUEST_REPLY = 1
    private const val REQUEST_MARK_AS_READ = 2

    private val denebPerson: Person =
        Person.Builder().setName("데네브").setKey("deneb").setBot(true).setImportant(true).build()
    private val userPerson: Person = Person.Builder().setName("나").setKey("deneb-user").build()

    /** Posts a new incoming message, appending to the live conversation if one is in the tray. */
    fun postIncoming(context: Context, title: String, body: String) {
        val style = (activeStyle(context) ?: newStyle())
            .setConversationTitle(title)
            .addMessage(body, System.currentTimeMillis(), denebPerson)
        post(context, style)
    }

    /**
     * Reflects the user's own reply in the conversation history. Called as soon
     * as the RemoteInput fires — before the gateway round-trip — so the tray/Auto
     * spinner clears immediately instead of hanging for a full agent turn.
     */
    fun appendUserReply(context: Context, replyText: String) {
        // Only update a conversation still in the tray: Android Auto can fire
        // mark-as-read (cancel) BEFORE the voice reply, and re-posting here
        // would resurrect an outgoing-only thread the user already dismissed.
        val style = activeStyle(context) ?: return
        post(context, style.addMessage(replyText, System.currentTimeMillis(), userPerson))
    }

    /** Re-posts the conversation with a Korean failure notice when a reply could not be delivered. */
    fun appendFailureNotice(context: Context) {
        val style = (activeStyle(context) ?: newStyle())
            .addMessage(
                "답장을 전송하지 못했습니다. 앱에서 다시 시도해 주세요.",
                System.currentTimeMillis(),
                denebPerson,
            )
        post(context, style)
    }

    /** Mark-as-read: dismiss the conversation from the tray / Auto. */
    fun cancel(context: Context) {
        notificationManager(context).cancel(NOTIFICATION_ID)
    }

    private fun newStyle(): NotificationCompat.MessagingStyle = NotificationCompat.MessagingStyle(userPerson).setGroupConversation(false)

    /**
     * Recovers the MessagingStyle of the notification currently in the tray (if
     * any) so consecutive pushes and replies accumulate as one conversation
     * instead of replacing each other. MessagingStyle caps history at 25
     * messages internally.
     */
    private fun activeStyle(context: Context): NotificationCompat.MessagingStyle? = runCatching {
        val active = notificationManager(context).activeNotifications
            .firstOrNull { it.id == NOTIFICATION_ID }
            ?.notification
            ?: return@runCatching null
        // extractMessagingStyleFromNotification unparcels the tray notification's
        // extras and can throw on a malformed/foreign payload. This runs on the
        // FCM background thread (postIncoming ← onMessageReceived), so an uncaught
        // throw would crash the app on push delivery — keep the extract inside the
        // guard so it degrades to a fresh conversation instead.
        NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(active)
    }.getOrNull()

    private fun post(context: Context, style: NotificationCompat.MessagingStyle) {
        val manager = notificationManager(context)
        ensureAiNotificationChannel(manager)
        if (!canPostNotifications(context, AI_NOTIFICATION_CHANNEL_ID)) return

        val builder = NotificationCompat.Builder(context, AI_NOTIFICATION_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setStyle(style)
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .setAutoCancel(true)
            .addAction(replyAction(context))
            .addAction(markAsReadAction(context))
        contentIntent(context)?.let { builder.setContentIntent(it) }

        runCatching { manager.notify(NOTIFICATION_ID, builder.build()) }
    }

    /**
     * Same phone-tray tap behavior as the legacy proactive path: open the app
     * and deep-link into the 업무 topic where the report was mirrored.
     */
    private fun contentIntent(context: Context): PendingIntent? {
        val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)?.apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            putExtra(EXTRA_OPEN_WORK_TOPIC, true)
        } ?: return null
        return PendingIntent.getActivity(
            context,
            NOTIFICATION_ID,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun replyAction(context: Context): NotificationCompat.Action {
        val remoteInput = RemoteInput.Builder(KEY_REPLY_TEXT).setLabel("답장").build()
        val intent = Intent(context, DenebReplyReceiver::class.java).setAction(ACTION_REPLY)
        val pending = PendingIntent.getBroadcast(
            context,
            REQUEST_REPLY,
            intent,
            // RemoteInput needs a MUTABLE PendingIntent so the system can attach
            // the reply text (Android 12+ rejects RemoteInput on immutable ones).
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
        )
        return NotificationCompat.Action.Builder(R.drawable.ic_notification, "답장", pending)
            .setSemanticAction(NotificationCompat.Action.SEMANTIC_ACTION_REPLY)
            .setShowsUserInterface(false)
            .setAllowGeneratedReplies(true)
            .addRemoteInput(remoteInput)
            .build()
    }

    private fun markAsReadAction(context: Context): NotificationCompat.Action {
        val intent = Intent(context, DenebReplyReceiver::class.java).setAction(ACTION_MARK_AS_READ)
        val pending = PendingIntent.getBroadcast(
            context,
            REQUEST_MARK_AS_READ,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Action.Builder(R.drawable.ic_notification, "읽음", pending)
            .setSemanticAction(NotificationCompat.Action.SEMANTIC_ACTION_MARK_AS_READ)
            .setShowsUserInterface(false)
            .build()
    }

    private fun notificationManager(context: Context): NotificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
}
