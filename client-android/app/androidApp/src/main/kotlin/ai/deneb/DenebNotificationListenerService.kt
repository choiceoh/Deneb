package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.ingestEvent
import ai.deneb.network.httpTeardownTolerantHandler
import android.app.Notification
import android.app.NotificationManager
import android.app.RemoteInput
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import android.util.Log
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
 * Beyond the basic title/body pair, extraction reads the structured extras
 * (big text, inbox lines, MessagingStyle messages) and applies app-specific
 * formatting for the two highest-signal work apps: KakaoTalk chats (room +
 * per-sender messages) and Amaranth10 electronic-approval notifications
 * (status/title/requester fields). Structured payloads are cumulative on the
 * Android side — each update re-carries the retained message list — so a
 * line-level dedup keeps already-forwarded lines from being resent.
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

    // onDestroy's scope.cancel() can catch an ingestEvent RPC mid-flight — the
    // handler keeps that stream teardown from crashing the app.
    private val scope = CoroutineScope(
        SupervisorJob() + Dispatchers.Default + httpTeardownTolerantHandler("NotificationListener"),
    )
    private val repository: DataRepository by inject(DataRepository::class.java)

    override fun onNotificationPosted(sbn: StatusBarNotification?, rankingMap: RankingMap?) {
        // A listener callback must never throw. Extracting another app's
        // notification forces a cross-process Bundle unparcel, which raises
        // BadParcelableException/ClassNotFoundException whenever the payload
        // carries a custom Parcelable whose class isn't in our process — and an
        // uncaught throw here kills the whole app process (surfacing as the
        // random background crashes seen after structured extraction landed).
        // Swallow + log so one hostile or malformed notification degrades to
        // "skipped", never a crash. readNotificationText guards the specific
        // unparcel site too; this is the process-death backstop for everything else.
        val event = runCatching { extractEvent(sbn, rankingMap) }
            .onFailure { Log.w(TAG, "notification extraction failed; skipping", it) }
            .getOrNull() ?: return
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
            // A single-app burst keeps its real source label (the gateway's per-source
            // rules — e.g. the Gmail drop — must still apply to batches); only a mixed
            // burst becomes "여러 앱". Blocks stay structured instead of flattened lines.
            val source = batch.map { it.source }.distinct().singleOrNull() ?: "여러 앱"
            val blocks = batch.joinToString("\n\n") { "[${it.source}]\n${it.text}" }
            runCatching { client.ingestEvent("notification", source, "알림 ${batch.size}건 도착:\n$blocks") }
        } else {
            for (ev in batch) {
                runCatching { client.ingestEvent("notification", ev.source, ev.text) }
            }
        }
    }

    override fun onListenerConnected() {
        super.onListenerConnected()
        NotificationReplyBridge.replier = ::replyToRoom
    }

    override fun onListenerDisconnected() {
        NotificationReplyBridge.replier = null
        super.onListenerDisconnected()
    }

    override fun onDestroy() {
        NotificationReplyBridge.replier = null
        scope.cancel()
        super.onDestroy()
    }

    /**
     * Sends [text] into the live conversation named [room] by firing that
     * notification's own reply action.
     *
     * Looked up against `activeNotifications` at call time rather than from a
     * stored handle: a reply PendingIntent dies with its notification, so a
     * cached one would fail silently once the user reads or dismisses the chat.
     * Asking the live list means "not found" is an honest answer the agent can
     * relay ("그 대화 알림이 이미 사라졌습니다") instead of a false success.
     *
     * Requires the same NotificationListener access already granted for reading —
     * no new permission. Returns false when no live notification matches or it
     * carries no reply input (many apps post read-only notifications).
     */
    fun replyToRoom(room: String, text: String): Boolean = runCatching {
        val wanted = room.trim()
        if (wanted.isEmpty() || text.isBlank()) return false
        val live = activeNotifications ?: return false
        var match: StatusBarNotification? = null
        for (sbn in live) {
            val notification = sbn?.notification ?: continue
            if (!roomMatches(notification, wanted)) continue
            if (match != null) {
                // Two live notifications share this title — replying would pick
                // one at random and send to the wrong chat.
                Log.w(TAG, "notification reply ambiguous: multiple rooms named $wanted")
                return false
            }
            match = sbn
        }
        match?.notification?.let { sendReply(it, text) } ?: false
    }.onFailure { Log.w(TAG, "notification reply failed", it) }.getOrDefault(false)

    /** Matches the same room name the digest shows: conversation title, else title. */
    private fun roomMatches(notification: Notification, wanted: String): Boolean {
        val extras = notification.extras ?: return false
        val convo = extras.getCharSequence(Notification.EXTRA_CONVERSATION_TITLE)?.toString()?.trim().orEmpty()
        val title = extras.getCharSequence(Notification.EXTRA_TITLE)?.toString()?.trim().orEmpty()
        val room = convo.ifBlank { title }
        return room.isNotEmpty() && room.equals(wanted, ignoreCase = true)
    }

    /** Fires the notification's reply action with [text]. */
    private fun sendReply(notification: Notification, text: String): Boolean {
        val actions = notification.actions ?: return false
        for (action in actions) {
            val inputs = action?.remoteInputs?.filter { !it.resultKey.isNullOrEmpty() }.orEmpty()
            if (inputs.isEmpty() || action?.actionIntent == null) continue
            val results = Bundle().apply { inputs.forEach { putCharSequence(it.resultKey, text) } }
            val intent = Intent()
            RemoteInput.addResultsToIntent(inputs.toTypedArray(), intent, results)
            // Chat apps mark their reply action; sending to a non-reply action
            // (e.g. "mark as read") would silently do the wrong thing.
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P &&
                action.semanticAction != Notification.Action.SEMANTIC_ACTION_REPLY
            ) {
                continue
            }
            action.actionIntent.send(this, 0, intent)
            return true
        }
        return false
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

    // Line-level dedup for cumulative payloads: MessagingStyle / inbox-style extras
    // re-carry the whole retained message list on every update, so without this each
    // new chat message re-forwards all previous ones (1, then 1+2, then 1+2+3 …).
    // Keyed per package+conversation+line; a much longer window than the event dedup
    // because the retained list persists across many updates. Trade-off: a genuinely
    // repeated identical line ("네") in the SAME conversation within the window is
    // dropped from the event — acceptable for proactive sensing. Observing a line
    // refreshes its timestamp (the window slides), so a payload that keeps re-posting
    // keeps being suppressed — a line only becomes forwardable again after it has
    // been absent for a full window.
    private val forwardedLines = object : LinkedHashMap<String, Long>(128, 0.75f, true) {
        override fun removeEldestEntry(eldest: Map.Entry<String, Long>): Boolean = size > MAX_LINE_KEYS
    }

    private fun isFreshLine(scope: String, line: String): Boolean = synchronized(forwardedLines) {
        val now = System.currentTimeMillis()
        val key = "$scope|$line"
        val last = forwardedLines[key]
        forwardedLines[key] = now
        last == null || now - last >= LINE_DEDUP_WINDOW_MS
    }

    private fun withFreshCumulativeLines(pkg: String, details: NotificationText): NotificationText {
        if (!details.hasStructuredGroupPayload()) return details
        // Scoped by conversation (empty for non-conversation payloads) so the same
        // short line ("네") in two different rooms isn't cross-suppressed.
        val scope = "$pkg|${details.conversationTitle}"
        return details.copy(
            textLines = details.textLines.filter { isFreshLine(scope, it) },
            messages = details.messages.filter { isFreshLine(scope, it.formatted()) },
            // bigText can also be a cumulative transcript on messaging/inbox payloads
            // (and the formatters split it back into outbound lines), so its lines are
            // deduped too — but only here, when a structured payload marks the
            // notification as messaging/inbox style. A plain long body (mail text)
            // never reaches this path and is forwarded untouched.
            bigText = splitNotificationLines(details.bigText)
                .filter { isFreshLine(scope, it) }
                .joinToString("\n"),
        )
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
     *
     * Ordering matters: the cheap structural drops run first so the hot noise path
     * (media ticks, progress updates, secret notifications) never pays for extras
     * parsing or a PackageManager label lookup — and secret content is never read.
     */
    private fun extractEvent(sbn: StatusBarNotification?, rankingMap: RankingMap?): NotifEvent? {
        sbn ?: return null
        val pkg = sbn.packageName
        if (pkg == packageName) return null // our own notifications (feedback loop)
        val n = sbn.notification ?: return null

        if (n.flags and Notification.FLAG_ONGOING_EVENT != 0) return null // foreground service / media / downloads

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
        if (pkg in SENSITIVE_PACKAGES) return null
        if (n.visibility == Notification.VISIBILITY_SECRET) return null

        val source = appLabel(pkg)
        val raw = readNotificationText(n.extras)
        val kakaoTalk = isKakaoTalk(pkg, source)
        val amaranth10 = isAmaranth10(pkg, source)

        // Group summaries duplicate their children (KakaoTalk posts a per-room child
        // for every message a summary aggregates), so they are dropped — EXCEPT the
        // Amaranth10 batched-approval summary, whose inbox lines can be the only
        // carrier of the batch. Requires an actual approval signal + structured
        // payload so generic group headers still never leave the device.
        if (n.flags and Notification.FLAG_GROUP_SUMMARY != 0) {
            val approvalSummary = amaranth10 && hasApprovalSignal(raw) && raw.hasStructuredGroupPayload()
            if (!approvalSummary) return null
        }

        // Cumulative-payload guard: keep only lines not already forwarded. If the
        // structured payload existed but is entirely stale, the update carries
        // nothing new (a pure re-post) — drop it before it spends a judgment turn.
        val details = withFreshCumulativeLines(pkg, raw)
        if (raw.hasStructuredGroupPayload() && !details.hasStructuredGroupPayload()) return null

        val text = when {
            kakaoTalk -> formatKakaoTalkNotification(details)
            amaranth10 -> formatAmaranthNotification(details)
            else -> formatBasicNotification(details)
        }
        if (text.isBlank()) return null

        return NotifEvent(source = source, text = text, key = "$pkg|$text")
    }

    private fun readNotificationText(extras: Bundle?): NotificationText {
        if (extras == null) return NotificationText()
        // EXTRA_HISTORIC_MESSAGES is deliberately NOT read: historic = messages the
        // user has already seen before this update, so forwarding them only repeats
        // content an earlier event already carried.
        //
        // Each field is read independently (the text/textArray/messages helpers below
        // self-guard), so one un-deserializable extra — a custom Parcelable an app
        // stuffed into its OWN key — costs us only that field, not the readable ones.
        // On Android 13+ (lazy Bundle unparcel) reading a standard string key never
        // even touches a foreign key, so title/body/bigText come through intact and
        // the notification is still captured instead of dropped; on older Android the
        // first getter unparcels the whole Bundle eagerly, so a hostile payload yields
        // empty here (still no crash — onNotificationPosted is the process-death backstop).
        return NotificationText(
            title = extras.text(Notification.EXTRA_TITLE),
            body = extras.text(Notification.EXTRA_TEXT),
            bigText = extras.text(Notification.EXTRA_BIG_TEXT),
            subText = extras.text(Notification.EXTRA_SUB_TEXT),
            summaryText = extras.text(Notification.EXTRA_SUMMARY_TEXT),
            conversationTitle = extras.text(Notification.EXTRA_CONVERSATION_TITLE),
            textLines = extras.textArray(Notification.EXTRA_TEXT_LINES),
            messages = extras.messages(Notification.EXTRA_MESSAGES),
        )
    }

    // The app label already travels as the event's source field (the gateway prompt
    // renders it as 출처, and batches prefix each block with [source]), so none of
    // the formatters repeat it inside the text.
    //
    // Structured fields are forwarded too, not just title/body: bigText is the
    // untruncated body of an expanded notification (mail, SMS, long messages) and
    // wins over a shorter collapsed body; inbox lines and MessagingStyle messages
    // carry the actual items behind a generic "N개의 새 메시지" body. This also keeps
    // the line dedup honest — every line it marks as seen is actually sent.
    private fun formatBasicNotification(details: NotificationText): String {
        val body = if (details.bigText.length > details.body.length) details.bigText else details.body
        val messageLines = details.messages.map { it.formatted() }
        val messageTexts = details.messages.flatMap { splitNotificationLines(it.text) }.toSet()
        val supplemental = (splitNotificationLines(body) + details.textLines.flatMap(::splitNotificationLines))
            .filterNot { it in messageTexts }
        return distinctNonBlank(listOf(details.title) + messageLines + supplemental).joinToString("\n")
    }

    private fun formatKakaoTalkNotification(details: NotificationText): String {
        val lines = notificationLines(details)
        if (lines.isEmpty()) return ""
        val room = details.conversationTitle.ifBlank { details.title }
        val messageLines = kakaoTalkMessageLines(details, room)
        return buildString {
            if (room.isNotBlank()) append("대화방: ").append(room).append('\n')
            if (messageLines.isNotEmpty()) {
                append("메시지:\n")
                messageLines.forEach { append("- ").append(it).append('\n') }
            } else {
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

    // Amaranth10 electronic-approval notifications: surface the glance fields
    // (status/title/requester/department) the 비서실장 judgment needs, with the raw
    // lines preserved underneath for fidelity. Non-approval Amaranth notifications
    // fall through to the basic format.
    private fun formatAmaranthNotification(details: NotificationText): String {
        val lines = notificationLines(details)
        if (lines.none(::isApprovalLine)) return formatBasicNotification(details)
        val title = details.title.ifBlank { lines.firstOrNull().orEmpty() }
        val status = detectApprovalStatus(lines)
        val requester = extractField(lines, REQUESTER_LABELS)
        val department = extractField(lines, DEPARTMENT_LABELS)

        return buildString {
            append("종류: 전자결재\n")
            if (status.isNotBlank()) append("상태: ").append(status).append('\n')
            if (title.isNotBlank()) append("제목: ").append(title).append('\n')
            if (requester.isNotBlank()) append("기안자/요청자: ").append(requester).append('\n')
            if (department.isNotBlank()) append("부서: ").append(department).append('\n')
            append("본문:\n")
            lines.forEach { append(it).append('\n') }
        }.trimEnd()
    }

    // PackageManager lookups are cached — a busy stream re-resolves the same handful
    // of packages constantly. Labels are stable for the process lifetime (the
    // listener service is restarted on app updates anyway).
    private val appLabels = HashMap<String, String>()

    private fun appLabel(pkg: String): String = synchronized(appLabels) {
        appLabels.getOrPut(pkg) {
            runCatching {
                val pm = packageManager
                pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString()
            }.getOrNull()?.takeIf { it.isNotBlank() } ?: pkg
        }
    }

    private fun isKakaoTalk(pkg: String, label: String): Boolean = pkg == "com.kakao.talk" ||
        pkg.startsWith("com.kakao.talk.") ||
        label.contains("카카오톡") ||
        label.contains("KakaoTalk", ignoreCase = true)

    // Identified by package/label ONLY — deliberately no content sniffing, so another
    // app's notification that merely mentions 아마란스/결재 (a mail subject, a chat
    // message) is never mislabeled as an electronic-approval event.
    private fun isAmaranth10(pkg: String, label: String): Boolean = pkg in AMARANTH10_PACKAGES ||
        isAmaranthName(pkg) ||
        isAmaranthName(label)

    private fun isAmaranthName(value: String): Boolean = value.contains("amaranth", ignoreCase = true) ||
        value.contains("아마란스")

    private fun hasApprovalSignal(details: NotificationText): Boolean = notificationLines(details).any(::isApprovalLine)

    private fun isApprovalLine(line: String): Boolean = APPROVAL_KEYWORDS.any { line.contains(it) }

    private fun detectApprovalStatus(lines: List<String>): String {
        // Newline join so a compound status ("결재 완료") can't false-match across
        // two adjacent lines ("…결재" + "완료…").
        val merged = lines.joinToString("\n")
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

    // Each reader self-guards: a cross-process Bundle getter can throw
    // BadParcelableException when one value's class isn't in our process. Catching
    // per-field (instead of once around the whole read) lets a notification keep its
    // readable fields when only one extra is un-deserializable — see readNotificationText.
    private fun Bundle.text(key: String): String = runCatching { getCharSequence(key)?.toString()?.trim().orEmpty() }.getOrDefault("")

    private fun Bundle.textArray(key: String): List<String> = runCatching {
        getCharSequenceArray(key)?.mapNotNull { it?.toString()?.trim()?.takeIf(String::isNotEmpty) }.orEmpty()
    }.getOrDefault(emptyList())

    @Suppress("DEPRECATION")
    private fun Bundle.messages(key: String): List<NotificationMessage> = runCatching {
        getParcelableArray(key)?.mapNotNull { it as? Bundle }?.mapNotNull(::notificationMessage).orEmpty()
    }.getOrDefault(emptyList())

    private fun notificationMessage(bundle: Bundle): NotificationMessage? {
        val text = bundle.getCharSequence("text")?.toString()?.trim().orEmpty()
        if (text.isBlank()) return null
        val sender = bundle.getCharSequence("sender")?.toString()?.trim()?.takeIf(String::isNotEmpty)
            ?: senderPersonName(bundle)
            ?: ""
        return NotificationMessage(sender = sender, text = text)
    }

    // MessagingStyle stores the sender as a plain "sender" CharSequence pre-P and as
    // an android.app.Person under "sender_person" on P+ (API 28). minSdk is 26, so
    // the Person read is version-gated.
    @Suppress("DEPRECATION")
    private fun senderPersonName(bundle: Bundle): String? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.P) return null
        return runCatching {
            bundle.getParcelable<android.app.Person>("sender_person")
                ?.name
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

    private fun splitNotificationLines(value: String): List<String> = value.lineSequence()
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
        private const val TAG = "DenebNotifListener"

        private const val DEDUP_WINDOW_MS = 45_000L
        private const val MAX_DEDUP_KEYS = 200

        // Line-level dedup for cumulative payloads (MessagingStyle / inbox lines).
        // Much longer than DEDUP_WINDOW_MS: the retained message list keeps
        // re-appearing in every update until the user reads the conversation.
        private const val LINE_DEDUP_WINDOW_MS = 30 * 60_000L
        private const val MAX_LINE_KEYS = 600

        // Burst coalescing: buffer notifications this long, then forward the window's
        // events together — batched into one event when >= BATCH_THRESHOLD arrive. A
        // couple seconds is invisible for proactive sensing and collapses bursts.
        private const val COALESCE_WINDOW_MS = 2_000L
        private const val BATCH_THRESHOLD = 3

        // Amaranth10 (Douzone groupware) — package from the Play Store listing.
        val AMARANTH10_PACKAGES = setOf("com.douzone.bizbox.klago.app")

        // "결재" alone covers every 결재* compound (전자결재/결재요청/결재대기/미결재/결재함),
        // so only the non-결재 approval verbs are listed separately.
        val APPROVAL_KEYWORDS = listOf(
            "결재",
            "상신",
            "반려",
            "승인요청",
            "승인 요청",
        )

        // Ordered most-specific-first: detectApprovalStatus takes the first match, so
        // compound statuses must win over their "승인"/"완료" substrings.
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
