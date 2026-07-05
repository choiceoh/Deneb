package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import android.app.Notification
import android.app.NotificationManager
import android.os.Bundle
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
            val source = batch.map { it.source }.distinct().singleOrNull() ?: "여러 앱"
            val blocks = batch.joinToString("\n\n") { "[${it.source}]\n${it.text}" }
            runCatching { client.ingestEvent("notification", source, "알림 ${batch.size}건 도착:\n$blocks") }
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

    private data class NotificationText(
        val title: String = "",
        val body: String = "",
        val bigText: String = "",
        val subText: String = "",
        val summaryText: String = "",
        val conversationTitle: String = "",
        val textLines: List<String> = emptyList(),
        val messages: List<NotificationMessage> = emptyList(),
    ) {
        fun hasStructuredGroupPayload(): Boolean = textLines.isNotEmpty() || messages.isNotEmpty()
    }

    private data class NotificationMessage(
        val sender: String,
        val text: String,
    ) {
        fun formatted(): String = if (sender.isNotBlank()) "$sender: $text" else text
    }

    /**
     * On-device pre-filter: keep volume + cost down and exclude security-sensitive
     * notifications before anything leaves the device. The gateway also triages
     * OTP/spam, but this is the hygiene + noise floor (foreground/media/system/
     * group-summary/low-importance never make it to the server).
     */
    private fun extractEvent(sbn: StatusBarNotification?, rankingMap: RankingMap?): NotifEvent? {
        sbn ?: return null
        val sourcePackage = sbn.packageName
        if (sourcePackage == packageName) return null // our own notifications (feedback loop)
        val n = sbn.notification ?: return null
        val source = appLabel(sourcePackage)
        val details = readNotificationText(n.extras)
        val kakaoTalk = isKakaoTalk(sourcePackage, source)
        val amaranth10 = isAmaranth10(sourcePackage, source, details)
        val amaranthApproval = amaranth10 && hasApprovalSignal(details)
        val richGroupSummary = kakaoTalk || amaranthApproval

        if (n.flags and Notification.FLAG_ONGOING_EVENT != 0) return null // foreground service / media / downloads
        if (n.flags and Notification.FLAG_GROUP_SUMMARY != 0) {
            if (!richGroupSummary || !details.hasStructuredGroupPayload()) return null
        }

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
        if (sourcePackage in SENSITIVE_PACKAGES) return null
        if (n.visibility == Notification.VISIBILITY_SECRET) return null

        val text = when {
            kakaoTalk -> formatKakaoTalkNotification(details)
            amaranth10 -> formatAmaranthApprovalNotification(source, details)
                ?: formatBasicNotification(source, details)
            else -> formatBasicNotification(source, details)
        }
        if (text.isBlank()) return null

        return NotifEvent(source = source, text = text, key = sourcePackage + "|" + text)
    }

    private fun readNotificationText(extras: Bundle?): NotificationText {
        if (extras == null) return NotificationText()
        return NotificationText(
            title = extras.text(Notification.EXTRA_TITLE),
            body = extras.text(Notification.EXTRA_TEXT),
            bigText = extras.text(Notification.EXTRA_BIG_TEXT),
            subText = extras.text(Notification.EXTRA_SUB_TEXT),
            summaryText = extras.text(Notification.EXTRA_SUMMARY_TEXT),
            conversationTitle = extras.text(Notification.EXTRA_CONVERSATION_TITLE),
            textLines = extras.textArray(Notification.EXTRA_TEXT_LINES),
            messages = extras.messages(Notification.EXTRA_MESSAGES) +
                extras.messages(Notification.EXTRA_HISTORIC_MESSAGES),
        )
    }

    private fun formatBasicNotification(source: String, details: NotificationText): String {
        val lines = distinctNonBlank(listOf(details.title, details.body))
        if (lines.isEmpty()) return ""
        return buildString {
            append("앱: ").append(source).append('\n')
            append("본문:\n")
            append(lines.joinToString("\n"))
        }
    }

    private fun formatKakaoTalkNotification(details: NotificationText): String {
        val lines = notificationLines(details)
        if (lines.isEmpty()) return ""
        val room = details.conversationTitle.ifBlank { details.title }
        return buildString {
            append("앱: 카카오톡\n")
            if (room.isNotBlank()) append("대화방: ").append(room).append('\n')
            val messageLines = kakaoTalkMessageLines(details, room)
            if (messageLines.isNotEmpty()) {
                append("메시지:\n")
                messageLines.forEach { append("- ").append(it).append('\n') }
            } else {
                append("본문:\n")
                lines.filterNot { it == room }.forEach { append(it).append('\n') }
            }
        }.trimEnd()
    }

    private fun kakaoTalkMessageLines(details: NotificationText, room: String): List<String> {
        val messageTexts = details.messages.flatMap {
            splitNotificationLines(it.text) + splitNotificationLines(it.formatted())
        }.toSet()
        val structuredMessages = details.messages.map { it.formatted() }
        val supplementalLines = distinctNonBlank(
            details.textLines.flatMap(::splitNotificationLines) +
                listOf(details.bigText, details.body, details.summaryText).flatMap(::splitNotificationLines),
        ).filterNot { line ->
            line == room ||
                line == details.title ||
                line in messageTexts
        }
        return distinctNonBlank(structuredMessages + supplementalLines)
    }

    private fun formatAmaranthApprovalNotification(source: String, details: NotificationText): String? {
        val lines = notificationLines(details)
        if (lines.none(::isApprovalLine)) return null
        val title = details.title.ifBlank { lines.firstOrNull().orEmpty() }
        val status = detectApprovalStatus(lines)
        val requester = extractField(lines, REQUESTER_LABELS)
        val department = extractField(lines, DEPARTMENT_LABELS)

        return buildString {
            append("앱: ").append(source).append('\n')
            append("종류: 전자결재\n")
            if (status.isNotBlank()) append("상태: ").append(status).append('\n')
            if (title.isNotBlank()) append("제목: ").append(title).append('\n')
            if (requester.isNotBlank()) append("기안자/요청자: ").append(requester).append('\n')
            if (department.isNotBlank()) append("부서: ").append(department).append('\n')
            append("본문:\n")
            lines.forEach { append(it).append('\n') }
        }.trimEnd()
    }

    private fun appLabel(pkg: String): String = runCatching {
        val pm = packageManager
        pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString()
    }.getOrNull()?.takeIf { it.isNotBlank() } ?: pkg

    private fun isKakaoTalk(pkg: String, label: String): Boolean =
        pkg == "com.kakao.talk" ||
            pkg.startsWith("com.kakao.talk.") ||
            label.contains("카카오톡") ||
            label.contains("KakaoTalk", ignoreCase = true)

    private fun isAmaranth10(pkg: String, label: String, details: NotificationText): Boolean =
        pkg in AMARANTH10_PACKAGES ||
            isAmaranthName(pkg) ||
            isAmaranthName(label) ||
            notificationLines(details).any(::isAmaranthName)

    private fun isAmaranthName(value: String): Boolean =
        value.contains("amaranth", ignoreCase = true) ||
            value.contains("아마란스")

    private fun hasApprovalSignal(details: NotificationText): Boolean = notificationLines(details).any(::isApprovalLine)

    private fun isApprovalLine(line: String): Boolean =
        APPROVAL_KEYWORDS.any { line.contains(it) } ||
            (line.contains("승인") && line.contains("결재"))

    private fun detectApprovalStatus(lines: List<String>): String {
        val merged = lines.joinToString(" ")
        return APPROVAL_STATUSES.firstOrNull { merged.contains(it) }.orEmpty()
    }

    private fun extractField(lines: List<String>, labels: List<String>): String {
        for (line in lines) {
            for (label in labels) {
                val idx = line.indexOf(label)
                if (idx < 0) continue
                val value = line.substring(idx + label.length)
                    .trim()
                    .trimStart(':', '：', '-', ' ')
                    .trim()
                if (value.isNotBlank()) return value
            }
        }
        return ""
    }

    private fun Bundle.text(key: String): String = getCharSequence(key)?.toString()?.trim().orEmpty()

    private fun Bundle.textArray(key: String): List<String> =
        getCharSequenceArray(key)
            ?.mapNotNull { it?.toString()?.trim()?.takeIf(String::isNotEmpty) }
            .orEmpty()

    @Suppress("DEPRECATION")
    private fun Bundle.messages(key: String): List<NotificationMessage> =
        getParcelableArray(key)
            ?.mapNotNull { it as? Bundle }
            ?.mapNotNull(::notificationMessage)
            .orEmpty()

    private fun notificationMessage(bundle: Bundle): NotificationMessage? {
        val text = bundle.getCharSequence("text")?.toString()?.trim().orEmpty()
        if (text.isBlank()) return null
        val sender = bundle.getCharSequence("sender")?.toString()?.trim()
            ?: personName(bundle.get("sender_person"))
            ?: ""
        return NotificationMessage(sender = sender, text = text)
    }

    private fun personName(senderPerson: Any?): String? {
        if (senderPerson == null) return null
        return runCatching {
            senderPerson.javaClass
                .methods
                .firstOrNull { it.name == "getName" && it.parameterCount == 0 }
                ?.invoke(senderPerson)
                ?.toString()
                ?.trim()
                ?.takeIf(String::isNotEmpty)
        }.getOrNull()
    }

    private fun notificationLines(details: NotificationText): List<String> = distinctNonBlank(
        listOf(
            details.title,
            details.conversationTitle,
            details.body,
            details.bigText,
            details.subText,
            details.summaryText,
        ).flatMap(::splitNotificationLines) +
            details.textLines.flatMap(::splitNotificationLines) +
            details.messages.map { it.formatted() },
    )

    private fun splitNotificationLines(value: String): List<String> =
        value.lineSequence()
            .map(String::trim)
            .filter(String::isNotEmpty)
            .toList()

    private fun distinctNonBlank(values: List<String>): List<String> {
        val seen = LinkedHashSet<String>()
        for (value in values) {
            val cleaned = value.trim()
            if (cleaned.isNotEmpty()) seen += cleaned
        }
        return seen.toList()
    }

    private companion object {
        private const val DEDUP_WINDOW_MS = 45_000L
        private const val MAX_DEDUP_KEYS = 200

        // Burst coalescing: buffer notifications this long, then forward the window's
        // events together — batched into one event when >= BATCH_THRESHOLD arrive. A
        // couple seconds is invisible for proactive sensing and collapses bursts.
        private const val COALESCE_WINDOW_MS = 2_000L
        private const val BATCH_THRESHOLD = 3

        val AMARANTH10_PACKAGES = setOf("com.douzone.bizbox.klago.app")
        val APPROVAL_KEYWORDS = listOf(
            "결재",
            "전자결재",
            "결재요청",
            "결재 요청",
            "결재대기",
            "결재 대기",
            "승인요청",
            "승인 요청",
            "미결재",
            "상신",
            "반려",
            "결재함",
        )
        val APPROVAL_STATUSES = listOf(
            "결재 완료",
            "결재 요청",
            "결재요청",
            "결재 대기",
            "결재대기",
            "승인 요청",
            "승인요청",
            "미결재",
            "미결",
            "결재함",
            "상신",
            "반려",
            "승인",
            "완료",
        )
        val REQUESTER_LABELS = listOf("기안자", "상신자", "요청자", "작성자")
        val DEPARTMENT_LABELS = listOf("부서", "소속", "기안부서", "작성부서")

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
