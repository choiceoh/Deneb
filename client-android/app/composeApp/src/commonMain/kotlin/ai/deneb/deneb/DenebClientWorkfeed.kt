package ai.deneb.deneb

import ai.deneb.sensing.readCurrentLocation
import ai.deneb.sensing.readWorkUsageDigest
import ai.deneb.ui.chat.WorkFeedItem
import io.ktor.client.call.body
import io.ktor.client.request.get
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.put
import kotlin.time.TimeSource
import kotlin.uuid.Uuid

// Work-feed + native state sync surface of DenebGatewayClient, moved out of
// the class body into extensions (DenebClientMail/Fleet pattern). Logic is
// unchanged; state stays on the client as internal backing flows.

/**
 * Events-stream (re)connect = network back / app foregrounded. Pull the
 * open conversation's transcript so an answer that completed while the chat
 * SSE was dead becomes visible without an app restart. Skipped while an
 * ask() is in flight — its own stream (or recovery) owns the view then; the
 * epoch guard inside loadTranscriptGuarded covers one that starts mid-fetch.
 */
internal fun DenebGatewayClient.reconcileOpenConversationAsync() {
    if (askActive) return
    val key = sessionKey
    if (key.isBlank()) return
    scope.launch { runCatching { loadTranscriptGuarded(key) } }
}

/**
 * Open the client:main home conversation where proactive reports are mirrored
 * — the deep-link target when the user taps a proactive-report push. Guarded so
 * a concurrent cold-start share can't be clobbered (see historyGate).
 */
fun DenebGatewayClient.openWorkTopic() {
    switchSession("client:main")
    syncNativeStateAsync()
    // Deep-link switch to the work home: replace whatever conversation was open
    // (cold-start callers are already empty-guarded, so this is a no-op there).
    scope.launch { loadTranscriptGuarded("client:main", replacing = true) }
    loadConversations()
}

/** Mint a fresh independent session key branching off the home (client:main:<uuid>). */

/**
 * Open the client:main 업무 home positioned at the transcript message that
 * mirrors a proactive work-feed card, with its collapsed accordion rewritten
 * to open expanded — so tapping the card reads the report in the 업무 chat
 * instead of spawning a side-conversation (#2110 behavior, kept for capture
 * cards whose results have no transcript mirror). Returns the History id the
 * chat list should scroll to, or null when the mirror can't be located (the
 * caller then simply lands at the bottom, plain [openWorkTopic] behavior).
 * Epoch-guarded like [loadTranscriptGuarded] so a concurrent send isn't
 * clobbered.
 */
suspend fun DenebGatewayClient.openWorkTopicAtItem(item: WorkFeedItem): String? {
    switchSession("client:main")
    syncNativeStateAsync()
    val epoch = credEpoch
    val startEpoch = historyGate.withLock { historyEpoch }
    val transcript = fetchTranscript("client:main") ?: emptyList()
    val idx = indexOfMirroredReport(transcript, item.createdAtMs)
    val resolved = if (idx >= 0) {
        transcript.mapIndexed { i, h ->
            if (i == idx) h.copy(content = expandCollapsedReportFence(h.content)) else h
        }
    } else {
        transcript
    }
    historyGate.withLock {
        if (historyEpoch != startEpoch) return null
        if (epoch != credEpoch) return null // credentials switched — don't install the old account's transcript
        _chatHistory.value = resolved
    }
    return if (idx >= 0) resolved[idx].id else null
}

fun DenebGatewayClient.refreshWorkFeedAsync() {
    scope.launch { refreshWorkFeed() }
}

fun DenebGatewayClient.refreshWorkFeedRangeAsync(sinceMs: Long, beforeMs: Long) {
    scope.launch { refreshWorkFeed(sinceMs = sinceMs, beforeMs = beforeMs, merge = true) }
}

fun DenebGatewayClient.syncNativeStateAsync() {
    scope.launch { syncNativeState() }
}

suspend fun DenebGatewayClient.syncNativeState(): Boolean {
    val epoch = credEpoch
    val reloadSessions = linkedSetOf<String>()
    var pulled = false
    var eventCount = 0
    // A server-side local-calendar mutation (agent tool, mail-proposal accept,
    // cron, or another client) rides the sync stream as a calendar.changed event.
    // It carries no payload — the client just refetches — so we collect it as a
    // flag here and force the post-gate warm to refresh, bypassing the throttle.
    // mail.changed (LMTP arrival, archive/trash from another client) forces the
    // same warm — it refreshes both home caches, which is the point.
    var calendarChanged = false
    var mailChanged = false
    // Server retention rotated past our pre-drain cursor: the retained-tail
    // events below still apply, but the pruned range in between is gone for
    // good — collected here, healed wholesale after the gate.
    var truncatedByRetention = false
    // wiki.changed carries the touched path: collected here (not applied inside
    // the gate — each repair is an RPC) and fed to the offline mirror below.
    val wikiChangedPaths = linkedSetOf<String>()
    nativeSyncGate.withLock {
        var cursor = nativeSyncCursor
        var keepGoing = true
        var pages = 0
        // Drain until the server reports no more (hasMore==false). The page cap is
        // a runaway guard only, sized above the server's retention window (~3,000
        // events, nativesync/store.go) so a long background stretch catches up in
        // ONE resume instead of silently stopping at 400 events (the old cap of 4
        // pages — M1's known loss window, battery doc §3.1). The non-advancing
        // cursor check below still breaks any pathological server loop early.
        while (keepGoing && pages < 40) {
            val payload = callRpc<NativeSyncPayload>(
                "miniapp.sync.pull",
                buildJsonObject {
                    put("cursor", cursor)
                    put("limit", 100)
                },
            ) ?: break
            // Credentials switched after this page returned: stop before applying
            // account A's events (work-feed/transcript mutations, notifications) or
            // advancing the cursor under account B.
            if (epoch != credEpoch) return false
            pulled = true
            eventCount += payload.events.size
            if (payload.truncated) truncatedByRetention = true
            payload.events.forEach { ev ->
                applyNativeSyncEvent(ev, reloadSessions)
                if (ev.type == "calendar.changed") calendarChanged = true
                if (ev.type == "mail.changed") mailChanged = true
                if (ev.type == "wiki.changed" && ev.entityId.isNotBlank()) wikiChangedPaths += ev.entityId
            }
            val nextCursor = payload.cursor.coerceAtLeast(cursor)
            if (nextCursor > nativeSyncCursor) {
                nativeSyncCursor = nextCursor
                appSettings.settings.putLong(DenebGatewayClient.KEY_SYNC_CURSOR, nextCursor)
            }
            keepGoing = payload.hasMore && nextCursor > cursor
            cursor = nextCursor
            pages++
        }
        // Credentials switched while this sync held the gate: onCredentialsChanged
        // reset the cursor/baseline OUTSIDE the gate, so a cursor we advanced above
        // could otherwise survive and make account B inherit account A's cursor.
        // Re-assert the reset here (still under the gate) so B replays from the start.
        if (epoch != credEpoch) {
            nativeSyncCursor = 0L
            appSettings.settings.putLong(DenebGatewayClient.KEY_SYNC_CURSOR, 0L)
            nativeSyncBaselined = false
            return false
        }
        // First successful pull is the catch-up baseline: from here on a
        // newly-created item raises a notification (the catch-up batch just
        // applied did not). Set inside the gate so the flag and the
        // maybeEmitProactiveNotification reads above stay serialized.
        if (pulled) nativeSyncBaselined = true
    }
    reloadSessions
        .filter { it == sessionKey }
        .forEach { loadTranscriptGuarded(it) }
    if (!pulled) {
        return refreshWorkFeed()
    }
    // An empty in-memory feed on a live gateway is always wrong — the server
    // keeps weeks of cards — it means the boot-time fetch lost a race
    // (gateway mid-redeploy, VPN still waking). Heal on ANY successful sync:
    // the old `eventCount == 0 &&` gate never fired on a busy system (there
    // are always fresh events), which left the feed stuck empty for days
    // (2026-07-05 field report).
    if (_denebWorkFeed.value.isEmpty()) {
        refreshWorkFeed()
    }
    // Retention truncation (battery doc §3.1): events between the pre-drain
    // cursor and the server's retained window were pruned before we saw them —
    // the drain above recovered only the retained tail. Anything state-shaped
    // may have drifted through the lost window (feed cards, calendar/mail,
    // wiki/org/approvals caches, the open transcript), so resync wholesale
    // from current server state instead of trusting the delta. Rare by
    // construction: only a multi-day absence outruns the ~3,000-event window.
    if (truncatedByRetention) {
        refreshWorkFeed()
        sectionCaches.clearAll()
        if (sessionKey.isNotBlank()) loadTranscriptGuarded(sessionKey)
        lastHomeWarm = null
    }
    // A calendar.changed/mail.changed event arrived: clear the throttle so the warm
    // below refreshes now rather than waiting out DenebGatewayClient.HOME_WARM_INTERVAL —
    // the home glance (and an open mail tab) should reflect the change immediately.
    if (calendarChanged || mailChanged) lastHomeWarm = null
    // The home warm flag only refreshes the "다가오는 일정" summary; the month grid
    // reads its own range cache, so it needs to be told too.
    if (calendarChanged) invalidateCalendar()
    // Reaching here means the gateway answered the pull, so it's reachable: warm the
    // rest of the home so the offline shell stays RECENT, not just last-visited. The
    // feed is already current (incremental sync events + the cold-prime above), but
    // calendar and mail only refreshed on screen entry — a long background stretch
    // then rendered a days-old glance on the next cold open. Throttled so bursty SSE
    // frames don't storm; each refresh owner-fingerprints + persists its own cache.
    warmHomeCachesThrottled()
    // Offline wiki mirror upkeep, inline like the warms above (this is already
    // a background coroutine; a detached launch would race the test harness):
    // targeted repairs for the pages this drain saw change, then the throttled
    // full refresh (seeds a fresh install, repairs anything the events missed).
    if (wikiChangedPaths.isNotEmpty()) updateWikiMirrorPaths(wikiChangedPaths)
    ensureWikiMirrorFresh()
    ensureDiaryMirrorFresh()
    maybeForwardUsageDigest()
    maybeForwardLocation()
    return true
}

// Background freshness for the offline launcher shell. Called only from a successful
// syncNativeState() (gateway reachable). Independent refreshes: a calendar failure
// must not starve mail. Both are credEpoch-fenced and persist their own caches.
private suspend fun DenebGatewayClient.warmHomeCachesThrottled() {
    lastHomeWarm?.let { if (it.elapsedNow() < DenebGatewayClient.HOME_WARM_INTERVAL) return }
    lastHomeWarm = TimeSource.Monotonic.markNow()
    refreshCalendar()
    refreshMail()
}

// Sensing: forward an on-device app-usage digest to the gateway as cache-only
// context. It must not create proactive notifications by itself; the assistant can
// read it through phone_read("usage") when it needs current work rhythm context.
// readWorkUsageDigest is a no-op (null) off Android or without Usage access; we
// still arm the throttle so we don't probe on every sync. Per-app switches are
// never sent — only this windowed, coarse digest.
private suspend fun DenebGatewayClient.maybeForwardUsageDigest() {
    lastUsageForward?.let { if (it.elapsedNow() < DenebGatewayClient.USAGE_FORWARD_INTERVAL) return }
    lastUsageForward = TimeSource.Monotonic.markNow()
    val digest = readWorkUsageDigest() ?: return
    ingestEvent("usage_update", "앱 사용 리듬", digest)
}

// Sensing: forward an on-demand location fix, throttled to DenebGatewayClient.LOCATION_FORWARD_INTERVAL.
// The gateway caches it (type location_update → no judgment turn) so phone_read
// ("location") answers without an SSH round-trip. readCurrentLocation is a no-op
// (null) off Android or without the location permission; we still arm the throttle
// so we don't probe FusedLocation on every sync.
private suspend fun DenebGatewayClient.maybeForwardLocation() {
    lastLocationForward?.let { if (it.elapsedNow() < DenebGatewayClient.LOCATION_FORWARD_INTERVAL) return }
    lastLocationForward = TimeSource.Monotonic.markNow()
    val fix = readCurrentLocation() ?: return
    ingestEvent("location_update", "", fix)
}

suspend fun DenebGatewayClient.refreshWorkFeed(sinceMs: Long = 0L, beforeMs: Long = 0L, merge: Boolean = false): Boolean {
    val epoch = credEpoch
    val ranged = sinceMs > 0L || beforeMs > 0L
    val payload = callRpc<WorkFeedPayload>(
        "miniapp.workfeed.list",
        buildJsonObject {
            put("limit", if (ranged) 100 else 20)
            if (sinceMs > 0L) put("sinceMs", sinceMs)
            if (beforeMs > 0L) put("beforeMs", beforeMs)
        },
    )
    if (payload == null) {
        // The attempt finished (failed); stop showing the first-load skeleton so
        // an unreachable gateway falls back to the empty state rather than hanging.
        _workFeedLoaded.value = true
        return false
    }
    if (epoch != credEpoch) return false // credentials switched — don't show the old account's work-feed
    val incoming = payload.items
        .filter { it.id.isNotBlank() }
        .distinctByLast { it.id }
    if (merge && ranged) {
        _denebWorkFeed.update { current ->
            val kept = current.filterNot { item ->
                (sinceMs <= 0L || item.createdAtMs >= sinceMs) &&
                    (beforeMs <= 0L || item.createdAtMs < beforeMs)
            }
            sortWorkFeedItems(kept + incoming)
        }
    } else {
        _denebWorkFeed.value = incoming
    }
    _workFeedLoaded.value = true
    // Persist the recent feed so the home renders it instantly on the next cold
    // start and survives an unreachable gateway (the offline-first launcher shell).
    storeCachedWorkFeed(_denebWorkFeed.value)
    return true
}

/** In-app browser in-place translation (en/ru → ko): ships the page's text
 *  segments to miniapp.web.translate and returns a SAME-length, SAME-order
 *  list of translations. Null on transport/auth failure or when the
 *  translation role is unwired; the JS bridge then keeps the originals. */
@Serializable
private data class TranslatePayload(val translated: List<String> = emptyList())

internal suspend fun DenebGatewayClient.translateSegments(segments: List<String>, targetLang: String = "ko"): List<String>? {
    if (segments.isEmpty()) return emptyList()
    val payload: TranslatePayload? = callRpc(
        "miniapp.web.translate",
        buildJsonObject {
            put("segments", buildJsonArray { segments.forEach { add(it) } })
            put("targetLang", targetLang)
        },
    )
    return payload?.translated
}

/** Observation plane (miniapp.observe.*): read the gateway's own behavior and
 *  recent logs for the settings 관찰 tab. Returns null on transport/auth failure. */
internal suspend fun DenebGatewayClient.observeBehavior(days: Int): ObserveBehavior? = callRpc("miniapp.observe.behavior", buildJsonObject { put("days", days) })

internal suspend fun DenebGatewayClient.observeLogs(level: String, limit: Int, days: Int = 0): ObserveLogsPayload? = callRpc(
    "miniapp.observe.logs",
    buildJsonObject {
        put("level", level)
        put("limit", limit)
        if (days > 0) put("days", days)
    },
)

/** Observation-plane liveness (capture/agentlog wiring, ring fill, 24h glance). */
internal suspend fun DenebGatewayClient.observeHealth(): ObserveHealth? = callRpc("miniapp.observe.health", buildJsonObject {})

suspend fun DenebGatewayClient.openWorkFeedItem(id: String): String? {
    // Opening a 업무 card runs its analysis in a dedicated side-conversation off
    // the client:main home — NOT in client:main itself. The old path adopted the
    // item's home session (client:main for proactive cards like the morning
    // letter), so the verbose open-prompt and the summary landed as visible turns
    // in the main 업무 chat. The open-prompt embeds the item's full context
    // (title/source/summary/body), so the fresh session is self-sufficient. The
    // key is stable per item id, so re-opening the same card resumes its thread
    // instead of spawning duplicates.
    val prompt = runWorkFeedAction(id, "open", adoptSession = false) ?: return null
    val target = workItemSessionKey(id)
    switchSession(target)
    loadTranscriptGuarded(target, replacing = true)
    return prompt
}

// Dedicated side-conversation key for a 업무 card. The suffix always starts
// with wf- so the drawer folds it under 카드 대화. ASCII ids slug in place;
// Hangul-only ids cannot slug, so they get wf-<uuid> instead of a bare uuid
// (which would leak into 내 대화).
internal fun DenebGatewayClient.workItemSessionKey(itemId: String): String {
    val slug = itemId.trim().lowercase()
        .map { if (it in 'a'..'z' || it in '0'..'9') it else '-' }
        .joinToString("")
        .trim('-')
        .take(40)
    return if (slug.isEmpty()) "client:main:wf-${Uuid.random()}" else "client:main:wf-$slug"
}

suspend fun DenebGatewayClient.runWorkFeedAction(
    itemId: String,
    actionId: String,
    comment: String? = null,
    adoptSession: Boolean = true,
): String? {
    if (itemId.isBlank() || actionId.isBlank()) return null
    val rejectionComment = comment
        ?.trim()
        ?.takeIf { actionId.trim() == "approval:reject" && it.isNotEmpty() }
    val payload = callRpc<WorkFeedActionRunPayload>(
        "miniapp.workfeed.action.run",
        buildJsonObject {
            put("itemId", itemId)
            put("actionId", actionId)
            if (rejectionComment != null) put("comment", rejectionComment)
        },
    ) ?: return null
    if (payload.removeFromFeed) {
        _denebWorkFeed.update { items -> items.filterNot { it.id == itemId } }
    } else if (payload.item.id.isNotBlank()) {
        _denebWorkFeed.update { items ->
            items.map { if (it.id == payload.item.id) payload.item else it }
        }
    }
    // The "open" caller routes to its own dedicated conversation, so it opts out
    // of adopting the item's home session here (client:main for proactive cards —
    // see openWorkFeedItem). Other actions still follow the server-returned key.
    val target = payload.sessionKey.ifBlank { payload.item.sessionKey }
    if (adoptSession && target.isNotBlank()) {
        switchSession(target)
        loadTranscriptGuarded(target, replacing = true)
    }
    return payload.prompt.ifBlank { null }
}

/**
 * Tray-action delivery (Trust Inbox): settles a card's approval action without
 * adopting its chat session — the notification path has no conversation to
 * route a prompt into. Reports success distinctly from a blank prompt, so a
 * background caller can tell "settled" from "delivery failed, card still
 * actionable in the feed".
 */
suspend fun DenebGatewayClient.runWorkFeedActionDurable(itemId: String, actionId: String): Boolean {
    if (itemId.isBlank() || actionId.isBlank()) return false
    val payload = callRpc<WorkFeedActionRunPayload>(
        "miniapp.workfeed.action.run",
        buildJsonObject {
            put("itemId", itemId)
            put("actionId", actionId)
        },
    ) ?: return false
    if (payload.removeFromFeed) {
        _denebWorkFeed.update { items -> items.filterNot { it.id == itemId } }
    } else if (payload.item.id.isNotBlank()) {
        _denebWorkFeed.update { items ->
            items.map { if (it.id == payload.item.id) payload.item else it }
        }
    }
    return true
}

/**
 * Free-text answer to a question card: settles the card and routes the typed
 * answer to the card's asking session (so the agent reacts to it). Mirrors
 * [runWorkFeedAction]'s adopt-session + return-prompt shape. Returns the answer
 * to deliver as a turn, or null on failure. Choice answers use runWorkFeedAction
 * instead (the chips are work-feed actions).
 */
suspend fun DenebGatewayClient.answerWorkFeedItem(itemId: String, answer: String): String? {
    if (itemId.isBlank() || answer.isBlank()) return null
    val payload = callRpc<WorkFeedActionRunPayload>(
        "miniapp.workfeed.answer",
        buildJsonObject {
            put("itemId", itemId)
            put("answer", answer)
        },
    ) ?: return null
    if (payload.removeFromFeed) {
        _denebWorkFeed.update { items -> items.filterNot { it.id == itemId } }
    }
    val target = payload.sessionKey.ifBlank { payload.item.sessionKey }
    if (target.isNotBlank()) {
        switchSession(target)
        loadTranscriptGuarded(target, replacing = true)
    }
    return payload.prompt.ifBlank { null }
}

/**
 * Sends a user correction on a work-feed card (long-press → 정정·피드백). The
 * gateway annotates the card in place with the correction and runs one agent
 * turn to fix the durable wiki knowledge. The returned (annotated) item is
 * upserted so the card reflects the correction; returns the agent's short
 * confirmation text (or null). Suspends until the gateway turn completes —
 * call from a background scope (the feed sheet closes optimistically).
 */
suspend fun DenebGatewayClient.sendWorkFeedFeedback(itemId: String, feedback: String): String? {
    if (itemId.isBlank() || feedback.isBlank()) return null
    val payload = callRpc<WorkFeedFeedbackPayload>(
        "miniapp.workfeed.feedback",
        buildJsonObject {
            put("itemId", itemId)
            put("feedback", feedback)
        },
    ) ?: return null
    if (payload.item.id.isNotBlank()) {
        _denebWorkFeed.update { items ->
            items.map { if (it.id == payload.item.id) payload.item else it }
        }
    }
    return payload.text.ifBlank { null }
}

/**
 * Regenerates a work-feed card's analysis (long-press → 다시 작성). The gateway
 * runs one agent turn that rewrites the analysis and replaces the card body in
 * place; the returned (rewritten) item is upserted so the card reflects it.
 * Suspends until the gateway turn completes — call from a background scope.
 */
suspend fun DenebGatewayClient.rewriteWorkFeedCard(itemId: String): String? {
    if (itemId.isBlank()) return null
    val payload = callRpc<WorkFeedFeedbackPayload>(
        "miniapp.workfeed.rewrite",
        buildJsonObject {
            put("itemId", itemId)
        },
    ) ?: return null
    if (payload.item.id.isNotBlank()) {
        _denebWorkFeed.update { items ->
            items.map { if (it.id == payload.item.id) payload.item else it }
        }
    }
    return payload.text.ifBlank { null }
}

/**
 * Marks a work-feed card read on the gateway (the user opened it). Softer than
 * ack — the card stays in the feed; this flips its readAtMs so the read state is
 * durable and shared across devices. The per-device seen-set drives the immediate
 * in-feed dim; the durable readAtMs lands on the next feed reload, and on other
 * devices via native sync. Fire-and-forget: returns true once the gateway accepted.
 */
suspend fun DenebGatewayClient.markWorkFeedRead(itemId: String): Boolean {
    if (itemId.isBlank()) return false
    return callRpc<JsonObject>(
        "miniapp.workfeed.read",
        buildJsonObject {
            put("itemId", itemId)
        },
    ) != null
}

/**
 * Forwards a captured phone event (the native NotificationListener's broad
 * notification capture) to the gateway's proactive judgment via
 * miniapp.event.ingest. The gateway triages — OTP/spam/routine stay silent,
 * signal lands in the work feed + push. Fire-and-forget: returns true once the
 * gateway accepted it (the judgment runs async server-side).
 */
suspend fun DenebGatewayClient.ingestEvent(type: String, source: String, text: String): Boolean {
    if (text.isBlank()) return false
    return callRpc<JsonObject>(
        "miniapp.event.ingest",
        buildJsonObject {
            put("type", type)
            put("source", source)
            put("text", text)
        },
    ) != null
}

private fun DenebGatewayClient.applyNativeSyncEvent(event: NativeSyncEvent, reloadSessions: MutableSet<String>) {
    when (event.type) {
        "workfeed.created" -> {
            val item = decodeWorkFeedItem(event.payload) ?: return
            upsertSyncedWorkFeedItem(item)
            maybeEmitProactiveNotification(item)
        }

        "workfeed.updated" -> {
            // Updates (status flips, action results) refresh the feed but are
            // not fresh arrivals, so they never raise a notification.
            val item = decodeWorkFeedItem(event.payload) ?: return
            upsertSyncedWorkFeedItem(item)
        }

        "workfeed.action.run" -> {
            val action = decodeWorkFeedActionRun(event.payload) ?: return
            if (action.removeFromFeed) {
                _denebWorkFeed.update { items -> items.filterNot { it.id == action.item.id } }
            } else {
                upsertSyncedWorkFeedItem(action.item)
            }
        }

        "transcript.appended" -> {
            if (event.sessionKey.isNotBlank()) {
                reloadSessions += event.sessionKey
            }
        }

        // Server-side wiki write/delete (agent tool, RPC, dreamer): drop the touched
        // page/category snapshots so the next view refetches instead of serving the
        // TTL-stale copy. Pure invalidation — no fetch here (the surface fetches on
        // view), so a burst of dreamer writes costs nothing beyond map removals.
        "wiki.changed" -> {
            val path = event.entityId
            if (path.isNotBlank()) {
                sectionCaches.wikiPages.invalidate(path)
                sectionCaches.categoryPages.invalidate(path.substringBeforeLast('/', missingDelimiterValue = ""))
            }
            // Page counts and cross-category lists (생성·이동·재분류) drift too —
            // cheap full drop; the next 카테고리 view refetches.
            sectionCaches.categories.invalidate()
        }

        // Org-chart save (this client, another client, the desktop workstation):
        // the chart also derives the dashboard's part lanes, so drop both.
        "org.changed" -> {
            sectionCaches.org.invalidate()
            sectionCaches.dashboard.invalidate()
        }

        // Approval-list drift the groupware radar observed (new pending, resolution,
        // new 수신참조): drop the list caches so the next 결재 view refetches.
        // (mail.changed is handled as a force-warm flag in syncNativeState, not here.)
        "approvals.changed" -> invalidateApprovalsListCache()
    }
}

private fun DenebGatewayClient.decodeWorkFeedItem(payload: JsonObject?): WorkFeedItem? {
    val item = payload?.get("item") ?: return null
    return runCatching { jsonCodec.decodeFromJsonElement(WorkFeedItem.serializer(), item) }.getOrNull()
}

private fun DenebGatewayClient.decodeWorkFeedActionRun(payload: JsonObject?): NativeSyncActionPayload? = runCatching {
    payload?.let { jsonCodec.decodeFromJsonElement(NativeSyncActionPayload.serializer(), it) }
}.getOrNull()

private fun DenebGatewayClient.upsertSyncedWorkFeedItem(item: WorkFeedItem) {
    if (item.id.isBlank()) return
    if (item.status == "acked" || item.status == "snoozed") {
        _denebWorkFeed.update { items -> items.filterNot { it.id == item.id } }
        return
    }
    _denebWorkFeed.update { items ->
        val next = items.filterNot { it.id == item.id } + item
        sortWorkFeedItems(next)
    }
}

private fun DenebGatewayClient.sortWorkFeedItems(items: List<WorkFeedItem>): List<WorkFeedItem> = items.sortedWith(
    compareByDescending<WorkFeedItem> { it.priority }
        .thenByDescending { it.createdAtMs }
        .thenByDescending { it.id },
)

// Raise a durable proactive notification for a freshly-created work-feed item.
// Called from applyNativeSyncEvent under nativeSyncGate, so the baseline read
// and the cursor advance are serialized — each item notifies at most once.
// Suppressed until the first sync has baselined (the catch-up over the closed
// period must not barrage) and only for live unread items (acked/snoozed are
// already dropped by upsertSyncedWorkFeedItem). tryEmit is non-blocking, so
// holding the gate here is safe.
private fun DenebGatewayClient.maybeEmitProactiveNotification(item: WorkFeedItem) {
    if (!nativeSyncBaselined) return
    if (item.id.isBlank() || item.status != "unread") return
    val body = item.summary.ifBlank { item.body }.ifBlank { item.title }
    _proactiveNotifications.tryEmit(
        DenebGatewayClient.ProactiveNotification(
            title = item.title.ifBlank { "Deneb" },
            body = body,
            ref = item.id,
            approveActionId = item.actions.firstOrNull { it.id.startsWith("approval:approve") }?.id,
            rejectActionId = item.actions.firstOrNull { it.id.startsWith("approval:reject") }?.id,
        ),
    )
}
