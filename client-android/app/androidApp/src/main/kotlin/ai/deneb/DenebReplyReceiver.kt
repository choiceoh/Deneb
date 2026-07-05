package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.koin.core.context.GlobalContext

/**
 * Handles the reply / mark-as-read actions of [DenebMessagingNotification] —
 * the path Android Auto drives for "voice reply" (the phone tray's inline reply
 * lands here too). Manifest-registered (exported=false) so it works even when
 * no activity is alive; Koin is available because DenebApplication.onCreate
 * runs for any process start.
 *
 * The reply text is forwarded to the gateway as a normal client:main chat
 * message via the shared [DenebGatewayClient], so the exchange lands in the
 * main work transcript like a typed message. Failure is surfaced by appending
 * a Korean notice to the conversation in the tray.
 */
class DenebReplyReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            DenebMessagingNotification.ACTION_MARK_AS_READ ->
                DenebMessagingNotification.cancel(context)

            DenebMessagingNotification.ACTION_REPLY -> handleReply(context, intent)
        }
    }

    private fun handleReply(context: Context, intent: Intent) {
        val text = RemoteInput.getResultsFromIntent(intent)
            ?.getCharSequence(DenebMessagingNotification.KEY_REPLY_TEXT)
            ?.toString()
            ?.trim()
            .orEmpty()
        if (text.isEmpty()) return

        val client = runCatching {
            GlobalContext.getOrNull()?.get<DataRepository>() as? DenebGatewayClient
        }.getOrNull()
        if (client == null) {
            DenebMessagingNotification.appendFailureNotice(context)
            return
        }

        // Echo the reply into the conversation right away: RemoteInput shows a
        // spinner until the notification is re-posted, and a full agent turn can
        // take a minute — far too long for the tray or a driving user.
        DenebMessagingNotification.appendUserReply(context, text)

        // goAsync keeps the process alive while the gateway round-trip runs.
        val pendingResult = goAsync()
        scope.launch {
            try {
                val ok = runCatching { client.sendDetachedChat(text) }.getOrDefault(false)
                if (!ok) DenebMessagingNotification.appendFailureNotice(context)
            } finally {
                pendingResult.finish()
            }
        }
    }

    private companion object {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    }
}
