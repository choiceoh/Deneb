package ai.deneb.deneb

import ai.deneb.data.Conversation
import ai.deneb.deneb.generated.SessionRowOut
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

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
/** A session belongs to the 챗봇 workspace iff its key is in the chat: namespace;
 *  everything else (client:main, cron:, system:, wf-…) is the 업무 workspace. */
internal fun isChatWorkspaceKey(key: String): Boolean = key.startsWith("chat:")

/** The two workspace home sessions — neither is deletable. */
internal fun isHomeSession(key: String): Boolean = key == "client:main" || key == "chat:main"

internal suspend fun DenebGatewayClient.fetchRecentSessions(): List<Conversation>? {
    // null return = RPC failed (timeout/transient/load). The caller keeps the
    // existing drawer list instead of collapsing to just the home row.
    val payload = callRpc<RecentPayload>(
        "miniapp.sessions.recent",
        buildJsonObject { put("limit", 50) },
    ) ?: return null
    // 업무 and 챗봇 keep SEPARATE session lists — show only the active workspace's
    // sessions. recall on = 업무, off = 챗봇 (the top-bar pill).
    val chatMode = !appSettings.isRecallEnabled()
    val recent = payload.sessions
        ?.filter { it.key.isNotBlank() && isChatWorkspaceKey(it.key) == chatMode }
        ?.map { s ->
            Conversation(
                id = s.key,
                messages = emptyList(),
                createdAt = s.startedAtMs?.takeIf { it > 0 } ?: s.updatedAtMs,
                updatedAt = s.updatedAtMs,
                title = conversationTitle(s),
            )
        }
        .orEmpty()
    // Pin the active workspace's home to the top (synthesize if absent), so it is
    // never missing once other sessions exist.
    val homeKey = if (chatMode) "chat:main" else "client:main"
    val homeTitle = if (chatMode) "챗봇" else "업무"
    val home = recent.find { it.id == homeKey }
        ?: Conversation(
            id = homeKey,
            messages = emptyList(),
            createdAt = 0,
            updatedAt = kotlin.time.Clock.System.now().toEpochMilliseconds(),
            title = homeTitle,
        )
    return listOf(home) + recent.filterNot { it.id == homeKey }
}

private fun DenebGatewayClient.conversationTitle(s: SessionRowOut): String {
    if (s.label.isNotBlank()) return s.label
    // The home sessions keep their workspace labels (match the empty-drawer
    // fallback), not "내 대화 · main".
    if (s.key == "client:main") return "업무"
    if (s.key == "chat:main") return "챗봇"
    // 챗봇 workspace side-conversations.
    if (isChatWorkspaceKey(s.key)) {
        val shortId = s.key.substringAfterLast(':').take(8)
        return if (shortId.isNotBlank() && shortId != "main") "챗봇 · $shortId" else "챗봇"
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
