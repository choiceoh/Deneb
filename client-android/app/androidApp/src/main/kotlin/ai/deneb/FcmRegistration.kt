package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import com.google.firebase.messaging.FirebaseMessaging
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * Registers this device's FCM registration token with the gateway so proactive
 * reports reach the phone when no live SSE connection is held (app fully closed
 * or in Doze) — the gateway-side fallback added in PR #2365
 * (gateway-go internal/domain/push).
 *
 * Best-effort and idempotent: the gateway dedups by token, so re-registering on
 * every foreground is cheap and keeps the stored token fresh. Everything is
 * wrapped so a build without google-services.json (desktop/CI, no Firebase app)
 * degrades to no-push instead of crashing.
 */
object FcmRegistration {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /** Fetch the current token and register it. Safe to call repeatedly. */
    fun fetchAndRegister(repository: DataRepository) {
        val client = repository as? DenebGatewayClient ?: return
        runCatching {
            FirebaseMessaging.getInstance().token.addOnSuccessListener { token ->
                register(client, token)
            }
        }
    }

    /** Register an explicit token (e.g. from FirebaseMessagingService.onNewToken). */
    fun register(client: DenebGatewayClient, token: String) {
        if (token.isBlank()) return
        scope.launch {
            runCatching { client.registerPushToken(token, "android") }
        }
    }
}
