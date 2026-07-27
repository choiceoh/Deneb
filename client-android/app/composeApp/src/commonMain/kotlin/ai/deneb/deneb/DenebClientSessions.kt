package ai.deneb.deneb

import ai.deneb.data.Attachment
import ai.deneb.data.Conversation
import ai.deneb.deneb.generated.SessionRowOut
import ai.deneb.deneb.generated.TranscriptMsgOut
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.stableTranscriptId
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.delay
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.time.TimeSource

/**
 * Sessions-drawer surface of [DenebGatewayClient] (`miniapp.sessions.recent`):
 * the recent-session list and its Korean row labels. Extensions so the gateway
 * client stays one facade while each RPC domain lives in its own file.
 */

// The right-side drawer is a pure session browser. It used to synthesize the
// configured topics (업무/잡담/코딩 from deneb.json topics.map) as pinned fake
// conversations at the top, but the topic switcher UI is gone and the client
// is a single client:main session model now — so that synthesis only leaked
// the retired topics back into the drawer. List real Deneb sessions only, and
// fall back to a lone client:main home when there are no sessions yet so the
// drawer is never empty.
/** Legacy 챗봇 namespace (chat:<uuid>) — the workspace was removed, but old
 *  conversations persist and stay listed/openable as ordinary sessions. */
internal fun isChatWorkspaceKey(key: String): Boolean = key.startsWith("chat:")

/** One drawer page. The gateway caps a single response at 100 rows. */
internal const val SESSION_PAGE = 50

/**
 * One page of the drawer list plus the size of the whole set, so the caller can
 * tell "that is all of them" from "the page ended". Conversations are no longer
 * garbage-collected server-side (#4353), so a page-only fetch would silently hide
 * every conversation past the first page as history grows.
 */
internal data class RecentSessionsPage(
    val conversations: List<Conversation>,
    val total: Int,
    // Rows the SERVER returned, before the locally-synthesized 업무 home row is
    // prepended — this, not the rendered size, is what the next offset counts.
    val serverRows: Int,
)

internal suspend fun DenebGatewayClient.fetchRecentSessions(offset: Int = 0): RecentSessionsPage? {
    // null return = RPC failed (timeout/transient/load). The caller keeps the
    // existing drawer list instead of collapsing to just the home row.
    val payload = callRpc<RecentPayload>(
        "miniapp.sessions.recent",
        buildJsonObject {
            put("limit", SESSION_PAGE)
            if (offset > 0) put("offset", offset)
        },
    ) ?: return null
    val recent = payload.sessions
        .filter { it.key.isNotBlank() }
        .map { s ->
            Conversation(
                id = s.key,
                messages = emptyList(),
                createdAt = s.startedAtMs?.takeIf { it > 0 } ?: s.updatedAtMs,
                updatedAt = s.updatedAtMs,
                title = conversationTitle(s),
            )
        }
    // An older gateway sends no `total` — fall back to what this page held so the
    // drawer stops offering more instead of paging into nothing.
    val total = payload.total ?: (offset + recent.size)
    // The 업무 home is pinned to the top of the FIRST page only; later pages append
    // verbatim (the server never repeats a row across offsets).
    if (offset > 0) return RecentSessionsPage(recent, total, recent.size)
    val home = recent.find { it.id == "client:main" }
        ?: Conversation(
            id = "client:main",
            messages = emptyList(),
            createdAt = 0,
            updatedAt = kotlin.time.Clock.System.now().toEpochMilliseconds(),
            title = "업무",
        )
    return RecentSessionsPage(listOf(home) + recent.filterNot { it.id == "client:main" }, total, recent.size)
}

private fun DenebGatewayClient.conversationTitle(s: SessionRowOut): String {
    if (s.label.isNotBlank()) return s.label
    // The 업무 home keeps its workspace label (matches the empty-drawer fallback),
    // not "내 대화 · main".
    if (s.key == "client:main") return "업무"
    // Legacy 챗봇 conversations (independent chat:<uuid>, retired workspace); an
    // unlabeled one reads as "대화 · <shortId>".
    if (isChatWorkspaceKey(s.key)) {
        val shortId = s.key.substringAfterLast(':').take(8)
        return if (shortId.isNotBlank()) "대화 · $shortId" else "대화"
    }
    // 업무-card side-conversations (opened from a feed card) read with the item's
    // title while it is still in the feed, falling back to a generic 업무 메모
    // label once the card is acked and dropped from the feed.
    if (s.key.substringAfterLast(':').startsWith("wf-")) {
        val itemTitle = _denebWorkFeed.value
            .firstOrNull { workItemSessionKey(it.id) == s.key }
            ?.title
            ?.trim()
            ?.takeIf { it.isNotBlank() }
            ?.take(40)
        return itemTitle?.let { "업무 · $it" } ?: "업무 메모"
    }
    val kind = s.key.substringBefore(':', "")
    // cron/system/boot carry meaning in the key itself — surface the job/kind
    // so the drawer reads "예약 · 메일 분석" / "시스템 부팅" instead of an opaque
    // "예약 작업 · 1780452…" timestamp suffix.
    when (kind) {
        "cron" -> return cronSessionLabel(s.key)
        "system" -> return systemSessionLabel(s.key)
        "boot" -> return "시스템 부팅"
    }
    val friendly = when (kind) {
        "client" -> "내 대화"
        else -> "대화"
    }
    val shortId = s.key.substringAfterLast(':').take(8)
    return if (shortId.isNotBlank()) "$friendly · $shortId" else friendly
}

// cron:<job>:<ts> → "예약 · <readable job>". Known jobs map to Korean; an
// unknown one falls back to the de-hyphenated job name so it still reads
// sensibly (e.g. cron:email-analysis-full:178… → "예약 · 메일 분석").
private fun cronSessionLabel(key: String): String {
    val job = key.split(':').getOrNull(1).orEmpty()
    return if (job.isBlank()) "예약 작업" else "예약 · ${prettyJobName(job)}"
}

// system:<name> → "시스템 · <name>" (e.g. system:heartbeat → 시스템 · 하트비트).
private fun systemSessionLabel(key: String): String {
    val name = key.substringAfter(':', "")
    return if (name.isBlank()) "시스템" else "시스템 · ${prettyJobName(name)}"
}

private fun prettyJobName(job: String): String = when {
    job.startsWith("email-analysis") -> "메일 분석"
    job.startsWith("morning") -> "모닝레터"
    job.startsWith("weekly") -> "주간 보고"
    job.startsWith("evening") -> "저녁 정리"
    job.startsWith("heartbeat") -> "하트비트"
    else -> job.replace('-', ' ')
}

// --- moved from DenebGatewayClient.kt (stage 2, logic unchanged): guarded
// transcript load + turn recovery, and session switch/fetch. ---

// --- Proactive-report deep link → session transcript --------------------

/**
 * Fetch a session transcript and install it as the chat history UNLESS an
 * optimistic send (ask()) appended while we were fetching. Closes the
 * cold-start share race: the topic auto-select's transcript fetch used to
 * overwrite the just-shared message and its streaming reply, so the share
 * appeared to get no response until the next message was sent. Epoch-checked
 * under historyGate, so it is safe whichever of load / send finishes first.
 */
internal suspend fun DenebGatewayClient.loadTranscriptGuarded(key: String, replacing: Boolean = false) {
    val startEpoch = historyGate.withLock { historyEpoch }
    // Pin the credential epoch: if the user switches gateways while this fetch is
    // in flight, both the view install and the cache write below are skipped, so an
    // old-account transcript can neither render under the new credentials nor
    // repopulate the just-cleared cache.
    val epoch = credEpoch
    // Cache-then-network: render the encrypted local copy instantly (no spinner).
    // [replacing] = an explicit switch to a different conversation (drawer pick,
    // proactive deep link): the view must reflect [key], so render its cache or
    // clear the previous session's rows even when there's no cache — otherwise a
    // failed fetch below would leave the prior conversation lingering under the
    // new sessionKey. When not replacing (cold-start restore / in-place reload)
    // only fill an empty view, so a live transcript never flashes. Either way the
    // epoch guard means an optimistic send (ask()) is never clobbered.
    val cached = loadCachedTranscript(key)
    historyGate.withLock {
        if (historyEpoch != startEpoch) return
        if (epoch != credEpoch) return // credentials switched — don't render the old account's cache
        when {
            // Switch: reflect the new key now — its cache, or clear the previous
            // session's rows so nothing lingers under the new key on a failed fetch.
            replacing -> _chatHistory.value = cached ?: emptyList()

            // Restore/reload in place: only fill an empty view from cache, so a
            // live transcript is never flashed over by a (possibly staler) snapshot.
            cached != null && _chatHistory.value.isEmpty() -> _chatHistory.value = cached
            // not replacing + (no cache or live view present): leave it for the network.
        }
    }
    val transcript = fetchTranscript(key) // null = RPC failure; [] = authoritative empty
    val authoritative = historyGate.withLock {
        if (historyEpoch != startEpoch) return // optimistic send won — don't touch view or cache
        if (epoch != credEpoch) return // credentials switched mid-flight — old account, drop it
        if (transcript != null) {
            _chatHistory.value = transcript
            true
        } else {
            false // transient failure: keep the instant cache render (or cleared view)
        }
    }
    // Reconcile the cache only for an authoritative fetch that won the epoch race,
    // so a stale (pre-send) transcript can never poison the cache. An authoritative
    // empty evicts any stale entry — e.g. a session deleted server-side — so it
    // can't resurrect on the next reopen. Skip entirely if credentials changed
    // mid-flight (this transcript belongs to the old account).
    if (authoritative && epoch == credEpoch) {
        if (transcript!!.isEmpty()) removeCachedTranscript(key) else storeCachedTranscript(key, transcript)
    }
}

/**
 * Stream-failure recovery for one sent turn: poll the session transcript
 * until it shows an assistant reply to [sentText], the message turns out
 * never to have arrived, or the budget runs out. The gateway keeps a turn
 * running after its SSE socket goes half-open (it cannot tell), so the
 * canonical answer usually lands in the transcript moments after the phone
 * loses the stream.
 *
 * An answer is only accepted once it is QUIESCENT — the same tail on two
 * consecutive polls — because a multi-step agent turn persists intermediate
 * assistant messages while its tools are still running. On budget expiry a
 * non-quiescent candidate is still installed (a partial real answer beats a
 * dropped turn). Returns [TurnRecoveryResult.NotArrived] only when the user
 * message never appeared — the sole case where a blocking re-send is safe.
 */
internal suspend fun DenebGatewayClient.recoverTurnFromTranscript(
    key: String,
    sentText: String,
): TurnRecoveryResult {
    val started = TimeSource.Monotonic.markNow()
    return recoverTurnFromTranscript(key, sentText) { started.elapsedNow().inWholeMilliseconds }
}

// elapsedMs is injected so the wall-clock budget boundary is unit-testable:
// TimeSource.Monotonic is a real clock (unaffected by runTest's virtual time), so
// only a stubbed elapsed can exercise the confirmed-running window extension.
internal suspend fun DenebGatewayClient.recoverTurnFromTranscript(
    key: String,
    sentText: String,
    elapsedMs: () -> Long,
): TurnRecoveryResult {
    var misses = 0
    var candidate: List<History>? = null
    var candidateTail: Pair<Int, String>? = null
    // Extend the poll window to the server turn deadline once the transcript has
    // confirmed the turn is still running: a tool-heavy turn outlives the short
    // budget, and giving up early strands the finished answer in the transcript
    // while the client freezes on the streamed preamble. The short budget still
    // caps the no-signal case below.
    var confirmedRunning = false
    while (true) {
        val budget = if (confirmedRunning) {
            DenebGatewayClient.STREAM_RECOVERY_MAX_MS
        } else {
            DenebGatewayClient.STREAM_RECOVERY_BUDGET_MS
        }
        if (elapsedMs() >= budget) break
        if (sessionKey != key) return TurnRecoveryResult.GiveUp // switched conversations — abandon
        val payload = fetchTranscriptPayload(key)
        if (payload != null) {
            val transcript = mapTranscriptMessages(payload.messages)
            val baseProbe = probeTranscriptForTurn(transcript, sentText)
            when (val probe = effectiveTurnProbe(baseProbe, payload.turnRunning)) {
                is TurnProbe.Answered -> {
                    val tail = transcript.size to probe.text
                    if (tail == candidateTail) {
                        installRecoveredTranscript(key, transcript)
                        return TurnRecoveryResult.Recovered(GatewayReply(text = probe.text, ok = true))
                    }
                    candidate = transcript
                    candidateTail = tail
                }

                TurnProbe.StillRunning -> {
                    confirmedRunning = true
                    misses = 0
                    candidate = null
                    candidateTail = null
                }

                TurnProbe.NotArrived -> {
                    // One grace poll: the POST may still be landing while we
                    // probe — declaring "never arrived" too early re-creates
                    // the duplicate-send this path exists to prevent.
                    misses++
                    if (misses >= 2) return TurnRecoveryResult.NotArrived
                }
            }
        }
        delay(DenebGatewayClient.STREAM_RECOVERY_POLL_MS)
    }
    val last = candidate
    val text = candidateTail?.second
    if (last != null && text != null) {
        installRecoveredTranscript(key, last)
        return TurnRecoveryResult.Recovered(GatewayReply(text = text, ok = true))
    }
    return TurnRecoveryResult.GiveUp
}

/** Install a recovered transcript as the visible history (and cache it). */
private suspend fun DenebGatewayClient.installRecoveredTranscript(key: String, transcript: List<History>) {
    historyGate.withLock {
        if (sessionKey != key) return // switched away — don't clobber the open view
        historyEpoch++ // fence any in-flight background load
        _chatHistory.value = transcript
    }
    storeCachedTranscript(key, transcript)
}

// Returns null on an RPC failure (so callers can keep a cache render instead of
// flashing to empty), or the messages — possibly an authoritative empty list —
// on success. The null-vs-[] distinction is what lets loadTranscriptGuarded
// evict a stale cache only when the server says the session is really empty.
internal suspend fun DenebGatewayClient.fetchTranscript(sessionKey: String): List<History>? = fetchTranscriptPayload(sessionKey)?.let { mapTranscriptMessages(it.messages) }

internal suspend fun DenebGatewayClient.fetchTranscriptPayload(sessionKey: String): TranscriptPayload? = callRpc<TranscriptPayload>(
    "miniapp.sessions.transcript",
    buildJsonObject {
        put("sessionKey", sessionKey)
        put("limit", 200)
    },
)

private fun mapTranscriptMessages(messages: List<TranscriptMsgOut>): List<History> = messages.mapNotNull { m ->
    val role = when (m.role.lowercase()) {
        "user" -> History.Role.USER
        "assistant" -> History.Role.ASSISTANT
        else -> return@mapNotNull null
    }
    val attachments = m.attachments
        .filter { it.data.isNotBlank() && it.mimeType.isNotBlank() }
        .map { Attachment(data = it.data, mimeType = it.mimeType, fileName = it.name.ifBlank { null }) }
        .toImmutableList()
    // Keep image-only proactive messages (e.g. the weekly-report form) even
    // when the caption is blank — they carry the attachment, not text.
    if (m.content.isBlank() && attachments.isEmpty()) {
        null
    } else {
        History(
            id = stableTranscriptId(role, m.content, m.timestampMs),
            role = role,
            content = m.content,
            attachments = attachments,
            timestampMs = m.timestampMs,
            reasoningContent = m.reasoning.ifBlank { null },
        )
    }
}
