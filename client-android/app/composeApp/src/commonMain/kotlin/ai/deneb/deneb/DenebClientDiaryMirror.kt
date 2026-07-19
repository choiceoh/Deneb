package ai.deneb.deneb

import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.time.TimeSource

/**
 * Offline diary mirror sync (see DiaryMirror.kt for the store): a paginated
 * bulk pull via miniapp.memory.diary_mirror seeds/refreshes the whole corpus.
 * Unlike the wiki mirror there is no change-event repair path — the diary is
 * append-only and low-churn, so the daily full pull is the only sync.
 */

// Runaway guard for the bulk pull: 20 pages × 300 rows admits far more than
// today's corpus (~2,350 entries).
private const val DIARY_MIRROR_MAX_PULL_PAGES = 20
private const val DIARY_MIRROR_PULL_LIMIT = 300

/**
 * Full mirror refresh when the mirror is missing or older than the shared
 * [WIKI_MIRROR_REFRESH_INTERVAL] (same cadence/attempt spacing as the wiki
 * mirror — both piggyback on the same sync tail). Called INLINE from a
 * successful native sync for the same harness-race reason as the wiki mirror.
 */
@OptIn(ExperimentalTime::class)
internal suspend fun DenebGatewayClient.ensureDiaryMirrorFresh() {
    if (diaryMirrorRefreshInFlight) return
    lastDiaryMirrorRefresh?.let { if (it.elapsedNow() < WIKI_MIRROR_ATTEMPT_INTERVAL) return }
    diaryMirrorRefreshInFlight = true
    lastDiaryMirrorRefresh = TimeSource.Monotonic.markNow()
    try {
        val syncedAt = diaryMirror.syncedAtMs()
        val ageMs = Clock.System.now().toEpochMilliseconds() - syncedAt
        if (syncedAt == 0L || ageMs > WIKI_MIRROR_REFRESH_INTERVAL.inWholeMilliseconds) {
            refreshDiaryMirrorFull()
        }
    } finally {
        diaryMirrorRefreshInFlight = false
    }
}

/**
 * Pull the whole diary via miniapp.memory.diary_mirror and atomically replace
 * the mirror. Any page failure aborts and keeps the previous mirror. Returns
 * true on success.
 */
@OptIn(ExperimentalTime::class)
internal suspend fun DenebGatewayClient.refreshDiaryMirrorFull(): Boolean {
    val epoch = credEpoch
    val expectedOwner = mailCacheOwner(gatewayUrl, clientToken)
    val all = mutableListOf<DiaryMirrorEntry>()
    var cursor = ""
    var pulls = 0
    var total = 0
    var syncComplete = false
    while (pulls < DIARY_MIRROR_MAX_PULL_PAGES) {
        val payload = callRpc<DiaryMirrorPayload>(
            "miniapp.memory.diary_mirror",
            buildJsonObject {
                if (cursor.isNotEmpty()) put("cursor", cursor)
                put("limit", DIARY_MIRROR_PULL_LIMIT)
            },
        ) ?: return false
        if (epoch != credEpoch) return false
        total = payload.total
        all += payload.entries
            .filter { it.file.isNotBlank() }
            .map { DiaryMirrorEntry(file = it.file, header = it.header, content = it.content, at = it.at) }
        if (!payload.hasMore) {
            syncComplete = true
            break
        }
        if (payload.nextCursor.isEmpty() || payload.nextCursor == cursor) return false
        cursor = payload.nextCursor
        pulls++
    }
    if (!syncComplete) return false
    // A scan that reports entries but emits none must not wipe a good mirror.
    if (all.isEmpty() && total > 0) return false
    if (epoch != credEpoch) return false
    return diaryMirror.replaceAll(
        all,
        Clock.System.now().toEpochMilliseconds(),
        expectedOwner = expectedOwner,
    )
}
