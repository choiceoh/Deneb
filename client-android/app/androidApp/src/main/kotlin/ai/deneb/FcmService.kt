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
 * The gateway sends DATA-ONLY messages (title/body in `data`, no `notification`
 * block), so onMessageReceived runs whenever FCM delivers (force-stop / OEM
 * background limits can still block delivery) and the app owns the
 * notification rendering. That is deliberate: Android Auto can only read aloud
 * and voice-reply to app-built MessagingStyle notifications with reply /
 * mark-as-read actions ([DenebMessagingNotification]) — a system-rendered
 * `notification` payload can never carry them.
 *
 * Token rotation: onNewToken re-registers so the gateway always holds a live
 * token for this device.
 */
class FcmService : FirebaseMessagingService() {
    private val repository: DataRepository by inject()

    override fun onNewToken(token: String) {
        (repository as? DenebGatewayClient)?.let { FcmRegistration.register(it, token) }
    }

    override fun onMessageReceived(message: RemoteMessage) {
        // The radio is already awake for this delivery — piggyback a home-widget
        // refresh (throttled, no-op without a placed widget) so the widget stays
        // fresh without its own alarm wakeups.
        WidgetRefresher.requestRefresh(this)
        // Data-first (current gateway); the notification-payload fallback keeps
        // an older gateway build working during a version skew window.
        val title = message.data["title"] ?: message.notification?.title ?: "데네브"
        val body = message.data["body"] ?: message.notification?.body ?: return
        DenebMessagingNotification.postIncoming(this, title, body)
    }
}
