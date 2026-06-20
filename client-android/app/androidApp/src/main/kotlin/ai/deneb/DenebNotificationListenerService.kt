package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import android.app.Notification
import android.app.NotificationManager
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.koin.java.KoinJavaComponent.inject

/**
 * Notification sensing — the work launcher's information-gathering background
 * service. Broadly captures posted notifications, drops the obvious noise and
 * security-sensitive ones on-device, and forwards the rest to the gateway via
 * miniapp.event.ingest. The gateway runs the proactive 비서실장 judgment (OTP/spam/
 * routine → silent NO_REPLY; signal → work feed + push), so the user only ever
 * sees signal. "다 읽되 다 보여주지 않는다": broad capture here, narrow surface server-side.
 *
 * A short coalescing window collapses notification bursts (group chat, batched
 * approvals) into a single event, so one burst costs one judgment turn, not N.
 *
 * Requires the user to grant Notification access (Settings > Notification access >
 * Deneb). FOSS-only — declared in the foss manifest, like the SMS/contacts features.
 * No re-notification happens here (the gateway's proactive layer owns delivery), so
 * broad capture never becomes user-facing noise.
 */
class DenebNotificationListenerService : NotificationListenerService() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val repository: DataRepository by inject(DataRepository::class.java)

    override fun onNotificationPosted(sbn: StatusBarNotification?, rankingMap: RankingMap?) {
        val event = extractEvent(sbn, rankingMap) ?: return
        if (isRecentDuplicate(event.key)) return // a re-posted / updated notification within the window
        enqueue(event)
    }

    // Burst coalescing: a group-chat burst or batched approvals fire many distinct
    // notifications within a second or two. Forwarding each individually spends one
    // gateway judgment turn per notification, so we buffer for a short window and
    // collapse a burst (>= BATCH_THRESHOLD in the window) into ONE event — mirroring
    // the Termux watcher's batch behavior. A lone notification just waits out the
    // window (~2s, negligible for proactive sensing) then forwards as-is. Guarded by
    // the `pending` monitor; notification callbacks can overlap.
    private val pending = mutableListOf<NotifEvent>()
    private var flushJob: Job? = null

    private fun enqueue(event: NotifEvent) {
        synchronized(pending) {
            pending.add(event)
            if (flushJob == null) {
                flushJob = scope.launch {
                    delay(COALESCE_WINDOW_MS)
                    flushAndForward()
                }
            }
        }
    }

    private suspend fun flushAndForward() {
        val batch: List<NotifEvent>
        synchronized(pending) {
            batch = pending.toList()
            pending.clear()
            flushJob = null
        }
        if (batch.isEmpty()) return
        val client = repository as? DenebGatewayClient ?: return
        // Fire-and-forget: the gateway acks immediately and judges async. A transport
        // failure (gateway down) just drops these notifications.
        if (batch.size >= BATCH_THRESHOLD) {
            val lines = batch.joinToString("\n") { "• " + "${it.source}: ${it.text}".replace("\n", " ").trim() }
            runCatching { client.ingestEvent("notification", "여러 앱", "알림 ${batch.size}건 도착:\n$lines") }
        } else {
            for (ev in batch) {
                runCatching { client.ingestEvent("notification", ev.source, ev.text) }
            }
        }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    // On-device dedup/throttle: apps re-post the same notification on every update
    // (a chat counter ticking, a media card, a sync banner), so without this the
    // gateway is flooded with near-identical judgment turns. Access-ordered + size-
    // bounded; skips a content key seen within the window. Notification callbacks can
    // overlap, so access is synchronized.
    private val recentlyForwarded = object : LinkedHashMap<String, Long>(64, 0.75f, true) {
        override fun removeEldestEntry(eldest: Map.Entry<String, Long>): Boolean = size > MAX_DEDUP_KEYS
    }

    private fun isRecentDuplicate(key: String): Boolean = synchronized(recentlyForwarded) {
        val now = System.currentTimeMillis()
        val last = recentlyForwarded[key]
        if (last != null && now - last < DEDUP_WINDOW_MS) return true
        recentlyForwarded[key] = now
        false
    }

    private data class NotifEvent(val source: String, val text: String, val key: String)

    /**
     * On-device pre-filter: keep volume + cost down and exclude security-sensitive
     * notifications before anything leaves the device. The gateway also triages
     * OTP/spam, but this is the hygiene + noise floor (foreground/media/system/
     * group-summary/low-importance never make it to the server).
     */
    private fun extractEvent(sbn: StatusBarNotification?, rankingMap: RankingMap?): NotifEvent? {
        sbn ?: return null
        if (sbn.packageName == packageName) return null // our own notifications (feedback loop)
        val n = sbn.notification ?: return null

        if (n.flags and Notification.FLAG_ONGOING_EVENT != 0) return null // foreground service / media / downloads
        if (n.flags and Notification.FLAG_GROUP_SUMMARY != 0) return null // group header duplicates its children

        when (n.category) {
            Notification.CATEGORY_TRANSPORT, // media playback controls
            Notification.CATEGORY_SERVICE,
            Notification.CATEGORY_PROGRESS, // downloads / uploads
            Notification.CATEGORY_SYSTEM,
            -> return null
        }

        // Low-importance channels are silent/ambient noise.
        if (rankingMap != null) {
            val ranking = Ranking()
            if (rankingMap.getRanking(sbn.key, ranking) && ranking.importance <= NotificationManager.IMPORTANCE_LOW) {
                return null
            }
        }

        // Security hygiene: never forward auth/secret notifications — the gateway
        // would also drop OTP, but these shouldn't leave the device at all.
        if (sbn.packageName in SENSITIVE_PACKAGES) return null
        if (n.visibility == Notification.VISIBILITY_SECRET) return null

        val extras = n.extras
        val title = extras?.getCharSequence(Notification.EXTRA_TITLE)?.toString()?.trim().orEmpty()
        val body = extras?.getCharSequence(Notification.EXTRA_TEXT)?.toString()?.trim().orEmpty()
        if (title.isEmpty() && body.isEmpty()) return null

        val text = listOf(title, body).filter { it.isNotEmpty() }.joinToString("\n")
        return NotifEvent(source = appLabel(sbn.packageName), text = text, key = sbn.packageName + "|" + text)
    }

    private fun appLabel(pkg: String): String = runCatching {
        val pm = packageManager
        pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString()
    }.getOrNull()?.takeIf { it.isNotBlank() } ?: pkg

    private companion object {
        private const val DEDUP_WINDOW_MS = 45_000L
        private const val MAX_DEDUP_KEYS = 200

        // Burst coalescing: buffer notifications this long, then forward the window's
        // events together — batched into one event when >= BATCH_THRESHOLD arrive. A
        // couple seconds is invisible for proactive sensing and collapses bursts.
        private const val COALESCE_WINDOW_MS = 2_000L
        private const val BATCH_THRESHOLD = 3

        // Best-effort hygiene blocklist: password managers / authenticators whose
        // notifications carry codes or vault access.
        val SENSITIVE_PACKAGES = setOf(
            "com.google.android.apps.authenticator2",
            "com.azure.authenticator",
            "com.authy.authy",
            "com.lastpass.lpandroid",
            "com.agilebits.onepassword",
            "com.bitwarden.authenticator",
            "com.x8bit.bitwarden",
        )
    }
}
