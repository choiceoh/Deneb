package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.sendDetachedChat
import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import org.koin.core.context.GlobalContext

/**
 * Durable delivery for a notification (voice) reply. A BroadcastReceiver-scoped
 * coroutine dies with the receiver process, and because the gateway binds the
 * turn to the request context, that death CANCELS the in-flight agent turn —
 * the reply would be lost outright. WorkManager persists the work across
 * process death and re-runs it until it completes.
 *
 * Retry policy: up to [MAX_ATTEMPTS] runs with exponential backoff. A re-run
 * after a mid-turn disconnect can duplicate the user line in the transcript
 * (the gateway persists the user message before the turn runs) — accepted:
 * a rare duplicated line beats a silently lost reply while driving.
 */
class DenebReplyWorker(
    context: Context,
    params: WorkerParameters,
) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val text = inputData.getString(KEY_TEXT)?.trim().orEmpty()
        if (text.isEmpty()) return Result.success()

        // Koin is up for any process start (DenebApplication.onCreate), but a
        // miswired cold start must not crash the worker — surface and stop.
        val client = runCatching {
            GlobalContext.getOrNull()?.get<DataRepository>() as? DenebGatewayClient
        }.getOrNull()
            ?: return giveUp()

        val ok = runCatching { client.sendDetachedChat(text) }.getOrDefault(false)
        return when {
            ok -> Result.success()
            runAttemptCount < MAX_ATTEMPTS - 1 -> Result.retry()
            else -> giveUp()
        }
    }

    private fun giveUp(): Result {
        DenebMessagingNotification.appendFailureNotice(applicationContext)
        return Result.failure()
    }

    companion object {
        const val KEY_TEXT = "reply_text"
        private const val MAX_ATTEMPTS = 3
    }
}
