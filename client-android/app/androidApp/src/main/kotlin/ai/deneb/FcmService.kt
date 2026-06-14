package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import org.koin.android.ext.android.inject

/**
 * Receives FCM messages and token rotations. The gateway only delivers here as a
 * fallback when no native client holds a live SSE connection (app fully closed /
 * Doze) — see gateway-go internal/domain/push.
 *
 * Delivery split:
 *  - App fully closed: the message carries a `notification` payload, so the
 *    system tray renders it directly and onMessageReceived is NOT called. That
 *    is the core gap this feature closes — no app code has to run.
 *  - App in foreground (rare — the gateway normally sees the live SSE subscriber
 *    and skips FCM): onMessageReceived fires; we raise the same proactive
 *    notification the SSE path would have.
 *  - Token rotation: onNewToken re-registers so the gateway always holds a live
 *    token for this device.
 */
class FcmService : FirebaseMessagingService() {
    private val repository: DataRepository by inject()

    override fun onNewToken(token: String) {
        (repository as? DenebGatewayClient)?.let { FcmRegistration.register(it, token) }
    }

    override fun onMessageReceived(message: RemoteMessage) {
        val title = message.notification?.title ?: message.data["title"] ?: "Deneb"
        val body = message.notification?.body ?: message.data["body"] ?: return
        // Reuses the shared proactive-notification path (channel, deep-link to the
        // work feed) from composeApp so foreground FCM looks identical to an
        // SSE-delivered report.
        sendProactiveReportNotification(title = title, body = body)
    }
}
