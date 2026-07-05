package ai.deneb

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.workDataOf
import java.util.concurrent.TimeUnit

/**
 * Handles the reply / mark-as-read actions of [DenebMessagingNotification] —
 * the path Android Auto drives for "voice reply" (the phone tray's inline reply
 * lands here too). Manifest-registered (exported=false) so it works even when
 * no activity is alive.
 *
 * The reply itself is delivered by [DenebReplyWorker] via WorkManager: an agent
 * turn can outlive both the broadcast window and this process, and a receiver-
 * scoped send would cancel the turn if Android reclaimed the process mid-flight.
 * Here we only echo the reply into the tray conversation (clears the RemoteInput
 * spinner instantly) and enqueue the durable send.
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

        DenebMessagingNotification.appendUserReply(context, text)

        val work = OneTimeWorkRequestBuilder<DenebReplyWorker>()
            .setInputData(workDataOf(DenebReplyWorker.KEY_TEXT to text))
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 10, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context).enqueue(work)
    }
}
