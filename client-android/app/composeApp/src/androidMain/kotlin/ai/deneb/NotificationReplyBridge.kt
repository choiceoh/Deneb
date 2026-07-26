package ai.deneb

/**
 * Lets the phone-action path reply to *another app's* notification.
 *
 * The notification listener lives in `androidApp` and this module cannot see it
 * (the dependency runs androidApp → composeApp, never back), so the listener
 * registers itself here when it connects and clears the slot when it dies. A
 * null slot simply means "no listener running" — the action reports failure
 * rather than pretending it sent something.
 *
 * Why the listener and not a stored handle: a reply action is a PendingIntent
 * that dies with its notification. Looking it up at reply time against the live
 * `activeNotifications` needs the service instance, and in exchange there is no
 * handle bookkeeping, no expiry window, and no chance of firing a stale intent.
 */
object NotificationReplyBridge {
    /**
     * Sends [text] to the conversation named [room], returning whether a live
     * notification with a reply action was found and fired. Set by
     * DenebNotificationListenerService.
     */
    @Volatile
    var replier: ((room: String, text: String) -> Boolean)? = null

    fun reply(room: String, text: String): Boolean {
        if (room.isBlank() || text.isBlank()) return false
        val fn = replier ?: return false
        return runCatching { fn(room, text) }.getOrDefault(false)
    }
}
