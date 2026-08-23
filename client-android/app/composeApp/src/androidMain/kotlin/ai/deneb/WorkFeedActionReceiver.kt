package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.runWorkFeedActionDurable
import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.java.KoinJavaComponent
import kotlin.time.Duration.Companion.seconds

/**
 * Trust Inbox tray actions (docs/research/improvement-ideas.md 4.7): the
 * 승인/거절 buttons HeartbeatNotifier attaches to a work-feed approval card's
 * notification land here. The gateway's action.run is idempotent-ish (the
 * self-correction ledger review is idempotent on a repeated status), so the
 * fire-and-forget contract of DenebGeofenceReceiver fits: goAsync() keeps the
 * process alive for one quick RPC. A failed delivery leaves the card
 * unsettled server-side — the in-app feed still shows it, so nothing is lost.
 * The tray notification is cancelled on tap either way; a reject that needs a
 * comment stays an in-app action (the tray path sends no comment).
 */
class WorkFeedActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_RUN_WORK_FEED_ACTION) return
        val itemId = intent.getStringExtra(EXTRA_WORK_FEED_ITEM_ID)?.trim().orEmpty()
        val actionId = intent.getStringExtra(EXTRA_WORK_FEED_ACTION_ID)?.trim().orEmpty()
        if (itemId.isEmpty() || actionId.isEmpty()) return

        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        manager.cancel(PROACTIVE_NOTIFICATION_ID)

        val client = runCatching { KoinJavaComponent.get<DataRepository>(DataRepository::class.java) }
            .getOrNull() as? DenebGatewayClient ?: return

        val pending = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.Default).launch {
            try {
                withTimeoutOrNull(15.seconds) {
                    runCatching { client.runWorkFeedActionDurable(itemId, actionId) }
                }
            } finally {
                pending.finish()
            }
        }
    }

    companion object {
        const val ACTION_RUN_WORK_FEED_ACTION = "ai.deneb.RUN_WORK_FEED_ACTION"
        const val EXTRA_WORK_FEED_ITEM_ID = "ai.deneb.WORK_FEED_ITEM_ID"
        const val EXTRA_WORK_FEED_ACTION_ID = "ai.deneb.WORK_FEED_ACTION_ID"
    }
}
