package ai.deneb.deneb

import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.time.Clock
import kotlin.time.Duration.Companion.hours
import kotlin.time.Duration.Companion.minutes
import kotlin.time.ExperimentalTime
import kotlin.time.TimeSource

/**
 * Offline wiki mirror sync (see WikiMirror.kt for the store): a paginated bulk
 * pull seeds/refreshes the whole corpus, and wiki.changed sync events keep it
 * current page-by-page between refreshes. Extensions so DenebGatewayClient
 * stays one facade while each domain lives in its own file.
 */

/** Refresh cadence for the full pull — the safety net for anything the
 *  wiki.changed event stream missed (mirror seeded pre-event, ledger prune
 *  during a long offline stretch). */
internal val WIKI_MIRROR_REFRESH_INTERVAL = 24.hours

/** Minimum spacing between refresh ATTEMPTS — a failing pull (gateway up but
 *  wiki store sick) must not retry on every sync. */
internal val WIKI_MIRROR_ATTEMPT_INTERVAL = 10.minutes

// Runaway guard for the bulk pull: 40 pages × 300 rows admits far more than
// today's corpus (~554 pages).
private const val WIKI_MIRROR_MAX_PULL_PAGES = 40
private const val WIKI_MIRROR_PULL_LIMIT = 300

/**
 * Full mirror refresh when the mirror is missing or older than
 * [WIKI_MIRROR_REFRESH_INTERVAL]. Called INLINE from the tail of a successful
 * native sync (gateway reachable, already a background coroutine) rather than
 * launched — a detached coroutine would race the test harness's queued
 * replies, and there's nothing to gain from detaching (the sync is done).
 */
@OptIn(ExperimentalTime::class)
internal suspend fun DenebGatewayClient.ensureWikiMirrorFresh() {
    if (wikiMirrorRefreshInFlight) return
    lastWikiMirrorRefresh?.let { if (it.elapsedNow() < WIKI_MIRROR_ATTEMPT_INTERVAL) return }
    wikiMirrorRefreshInFlight = true
    lastWikiMirrorRefresh = TimeSource.Monotonic.markNow()
    try {
        val syncedAt = wikiMirror.syncedAtMs()
        val ageMs = Clock.System.now().toEpochMilliseconds() - syncedAt
        if (syncedAt == 0L || ageMs > WIKI_MIRROR_REFRESH_INTERVAL.inWholeMilliseconds) {
            refreshWikiMirrorFull()
        }
    } finally {
        wikiMirrorRefreshInFlight = false
    }
}

/**
 * Pull the whole corpus via miniapp.memory.mirror and atomically replace the
 * mirror. Any page failure aborts and keeps the previous mirror (a partial
 * corpus would render as missing pages offline). Returns true on success.
 */
@OptIn(ExperimentalTime::class)
internal suspend fun DenebGatewayClient.refreshWikiMirrorFull(): Boolean {
    val epoch = credEpoch
    val expectedOwner = mailCacheOwner(gatewayUrl, clientToken)
    val all = mutableListOf<WikiPage>()
    var cursor = ""
    var pulls = 0
    var total = 0
    var syncComplete = false
    while (pulls < WIKI_MIRROR_MAX_PULL_PAGES) {
        val payload = callRpc<WikiMirrorPayload>(
            "miniapp.memory.mirror",
            buildJsonObject {
                if (cursor.isNotEmpty()) put("cursor", cursor)
                put("limit", WIKI_MIRROR_PULL_LIMIT)
            },
        ) ?: return false
        if (epoch != credEpoch) return false
        total = payload.total
        all += payload.pages
            .filter { it.path.isNotBlank() }
            .map { it.toWikiPage() }
        if (!payload.hasMore) {
            syncComplete = true
            break
        }
        if (payload.nextCursor.isEmpty() || payload.nextCursor == cursor) return false
        cursor = payload.nextCursor
        pulls++
    }
    if (!syncComplete) return false
    // A scan that lists pages but emits none (transient wiki store read failure)
    // must not wipe a previously good mirror — offline browse would lose the corpus.
    if (all.isEmpty() && total > 0) return false
    // Credentials can switch after the last RPC but before the write — never stamp
    // account A's corpus with account B's owner fingerprint.
    if (epoch != credEpoch) return false
    return wikiMirror.replaceAll(
        all,
        Clock.System.now().toEpochMilliseconds(),
        expectedOwner = expectedOwner,
    )
}

/**
 * Targeted mirror repair for paths named by wiki.changed sync events: refetch
 * each page; a gateway NOT_FOUND means the page was deleted (remove it), while
 * a transport failure means offline (leave the mirror as-is — the next full
 * refresh repairs). No-ops until the first bulk seed so a cold client doesn't
 * build a one-page "mirror" that offline browse would mistake for the corpus.
 */
internal suspend fun DenebGatewayClient.updateWikiMirrorPaths(paths: Collection<String>) {
    if (wikiMirror.syncedAtMs() == 0L) return
    val epoch = credEpoch
    val expectedOwner = mailCacheOwner(gatewayUrl, clientToken)
    for (path in paths.filter { it.isNotBlank() }.distinct()) {
        if (epoch != credEpoch) return
        val outcome = callRpcOutcome<WikiPagePayload>(
            "miniapp.memory.get_page",
            buildJsonObject { put("path", path) },
        )
        if (epoch != credEpoch) return
        when (outcome) {
            is RpcOutcome.Ok -> if (!wikiMirror.upsert(
                    outcome.payload.toWikiPage(fallbackPath = path),
                    expectedOwner = expectedOwner,
                )) return
            is RpcOutcome.Rejected -> if (outcome.code == "NOT_FOUND" && !wikiMirror.remove(
                    path,
                    expectedOwner = expectedOwner,
                )) return
            RpcOutcome.Unreachable -> return // offline: stop burning the batch
        }
    }
}

internal fun WikiPagePayload.toWikiPage(fallbackPath: String = ""): WikiPage = WikiPage(
    path = path.ifBlank { fallbackPath },
    title = title.ifBlank { path.ifBlank { fallbackPath } },
    summary = summary,
    category = category,
    tags = tags,
    updated = updated,
    body = body,
    code = code,
)

internal fun WikiMirrorPageRow.toWikiPage(): WikiPage = WikiPage(
    path = path,
    title = title.ifBlank { path },
    summary = summary,
    category = category,
    tags = tags,
    updated = updated,
    body = body,
    code = code,
)
