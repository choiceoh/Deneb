package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.Conversation
import ai.deneb.data.DataRepository
import ai.deneb.data.MemoryEntry
import ai.deneb.data.RemoteDataRepository
import ai.deneb.data.ScheduledTask
import ai.deneb.data.UiSubmission
import ai.deneb.httpClient
import ai.deneb.data.Attachment
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.ui.chat.History
import kotlinx.collections.immutable.toImmutableList
import ai.deneb.ui.chat.WorkFeedItem
import io.github.vinceglb.filekit.PlatformFile
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.client.request.prepareGet
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.preparePost
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsChannel
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import io.ktor.utils.io.readUTF8Line
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.TimeSource
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * A [DataRepository] backed by the Deneb gateway.
 *
 * It delegates every non-chat member to [base] (the upstream RemoteDataRepository, kept
 * so settings and the rest keep working) and overrides the chat path plus the
 * conversation drawer to drive the gateway's `miniapp.*` RPC surface. The reply
 * text may carry a ```deneb-ui fence, which the upstream chat renderer turns into an
 * interactive screen.
 *
 * Auth uses the X-Deneb-Client-Token header. Generate the token on the gateway
 * host with `go run ./gateway-go/cmd/deneb-client-token` and set it, together
 * with the gateway URL, under the [KEY_URL] / [KEY_TOKEN] settings keys.
 *
 * This file is the core: chat send/stream, session + transcript management,
 * native sync / work feed, and the RPC transport ([callRpc] / [rpcWrite]). The
 * per-domain RPC surfaces live as extensions in the sibling DenebClient*.kt
 * files (mail, calendar/todo, memory/search, models/skills/crons, capture,
 * sessions browser) — they reach the transport and the backing StateFlows
 * through the internal members below.
 *
 * Revival pattern: to bring another dead upstream screen back to life, override the
 * DataRepository method(s) it calls and route them through [callRpc]. Flow-typed
 * members (chatHistory, savedConversations) map cleanly; synchronous getters need
 * a cached StateFlow refreshed off [scope].
 */
@OptIn(ExperimentalUuidApi::class)
class DenebGatewayClient(
    private val base: RemoteDataRepository,
    private val appSettings: AppSettings,
) : DataRepository by base {

    internal val jsonCodec = Json {
        ignoreUnknownKeys = true
        isLenient = true
    }

    internal val http = httpClient {
        install(ContentNegotiation) { json(jsonCodec) }
        install(HttpTimeout) {
            requestTimeoutMillis = REQUEST_TIMEOUT_MS
            // Fail fast when the gateway is unreachable instead of hanging the
            // full 180s request budget on a dead TCP connect. Streaming calls
            // set their own timeout{} and are unaffected.
            connectTimeoutMillis = CONNECT_TIMEOUT_MS
        }
    }

    // Background scope for fire-and-forget refreshes behind synchronous
    // DataRepository entry points (loadConversations / loadConversation).
    internal val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    internal val _chatHistory = MutableStateFlow<List<History>>(emptyList())
    override val chatHistory: StateFlow<List<History>> = _chatHistory

    // Guards _chatHistory against a background transcript load clobbering an
    // in-flight optimistic send. A cold-start share (onCreate, before the chat
    // UI exists) appends its message and starts streaming while a topic
    // auto-select is still fetching that topic's transcript; without this gate
    // the late fetch overwrote both the shared message and its streaming reply,
    // so the share showed NO response until the user sent another message.
    // ask() bumps the epoch when it appends; loadTranscriptGuarded only installs
    // its result when the epoch is unchanged, making the two order-independent.
    private val historyGate = Mutex()
    private var historyEpoch = 0L
    private val nativeSyncGate = Mutex()
    private var nativeSyncCursor = appSettings.settings.getLong(KEY_SYNC_CURSOR, 0L)

    private val _savedConversations = MutableStateFlow<List<Conversation>>(emptyList())
    override val savedConversations: StateFlow<List<Conversation>> = _savedConversations

    // Deneb wiki pages. getMemories() returns this snapshot and also kicks a refresh;
    // observers rebuild their state once the RPC lands.
    internal val _denebMemories = MutableStateFlow<List<MemoryEntry>>(emptyList())
    val denebMemories: StateFlow<List<MemoryEntry>> = _denebMemories

    // Deneb cron jobs surfaced through the upstream scheduler screen (same snapshot +
    // observe pattern as memory).
    internal val _denebScheduledTasks = MutableStateFlow<List<ScheduledTask>>(emptyList())
    val denebScheduledTasks: StateFlow<List<ScheduledTask>> = _denebScheduledTasks

    // Deneb model registry, exposed to the config screen's model switcher.
    internal val _denebModels = MutableStateFlow<List<ModelOption>>(emptyList())
    val denebModels: StateFlow<List<ModelOption>> = _denebModels

    // Current model id per role (main / lightweight / fallback) for the model tab.
    internal val _denebRoleModels = MutableStateFlow<Map<String, String>>(emptyMap())
    val denebRoleModels: StateFlow<Map<String, String>> = _denebRoleModels

    /** Model tuner advisories from miniapp.models.list ("provider/model: 권고"). */
    internal val _denebModelAdvisories = MutableStateFlow<List<String>>(emptyList())
    val denebModelAdvisories: StateFlow<List<String>> = _denebModelAdvisories

    internal val _denebSkills = MutableStateFlow<List<SkillRow>>(emptyList())
    val denebSkills: StateFlow<List<SkillRow>> = _denebSkills

    // Recent Gmail surfaced in the native mail screen.
    internal val _denebMail = MutableStateFlow<List<MailMessage>>(emptyList())
    val denebMail: StateFlow<List<MailMessage>> = _denebMail

    // Pagination cursor for the inbox; null when there are no more pages.
    internal val _denebMailNextToken = MutableStateFlow<String?>(null)
    val denebMailNextToken: StateFlow<String?> = _denebMailNextToken

    // Gmail query behind the current mail list (null = default inbox view).
    // Set by refreshMail on success; loadMoreMail must send the same query or
    // the next page would come from a different result set than the cursor.
    internal var denebMailActiveQuery: String? = null

    // Upcoming calendar events surfaced in the native calendar screen.
    internal val _denebCalendar = MutableStateFlow<List<CalendarEvent>>(emptyList())
    val denebCalendar: StateFlow<List<CalendarEvent>> = _denebCalendar

    // Native-client handshake snapshot: gateway version, active model, and
    // feature flags exposed by miniapp.client.hello.
    private val _clientStatus = MutableStateFlow<ClientStatus?>(null)
    val clientStatus: StateFlow<ClientStatus?> = _clientStatus

    // Native work feed: proactive reports and native shares as actionable rows.
    internal val _denebWorkFeed = MutableStateFlow<List<WorkFeedItem>>(emptyList())
    val denebWorkFeed: StateFlow<List<WorkFeedItem>> = _denebWorkFeed

    /** One proactive 업무-feed report worth a tray notification. */
    data class ProactiveNotification(val title: String, val body: String)

    // Durable proactive-notification stream. Emits once per genuinely-new
    // workfeed.created item the native-sync pull surfaces (see applyNativeSyncEvent
    // / maybeEmitProactiveNotification); TaskScheduler collects it to raise a tray
    // notification when backgrounded. The gateway's live SSE push is best-effort
    // with no persistence — a frame produced while the app is asleep, mid-reconnect,
    // or across a gateway restart is dropped and never replayed — so notifications
    // hang off the cursor-based sync instead, which replays every missed item
    // exactly once on the next pull (live-push-triggered, reconnect catch-up, or
    // the poll-loop fallback).
    private val _proactiveNotifications =
        MutableSharedFlow<ProactiveNotification>(extraBufferCapacity = 32)
    val proactiveNotifications: SharedFlow<ProactiveNotification> =
        _proactiveNotifications.asSharedFlow()

    // The first post-launch sync is a catch-up over everything accumulated while
    // the app was closed: surface those into the feed but suppress notifications so
    // opening the app doesn't fire a barrage. Only items pulled after this is set
    // raise a notification. Read/written only under nativeSyncGate, so the gate's
    // happens-before covers visibility without @Volatile.
    private var nativeSyncBaselined = false

    internal var sessionKey: String = "client:main"
        private set
    private val _currentConversationId = MutableStateFlow<String?>(sessionKey)
    override val currentConversationId: StateFlow<String?> = _currentConversationId

    internal val gatewayUrl: String
        get() = appSettings.settings.getString(KEY_URL, DEFAULT_URL).trimEnd('/')

    internal val clientToken: String
        get() = appSettings.settings.getString(KEY_TOKEN, "")

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?) {
        val displayText = question?.trim().orEmpty()
        // A deneb-ui button press arrives as a UiSubmission. Show the friendly
        // question in the chat, but send the agent a structured callback naming
        // the event (per the deneb-ui prompt contract) plus the collected inputs.
        val sendText = if (uiSubmission != null) formatCallback(uiSubmission) else displayText
        if (sendText.isEmpty()) return

        // Append the user message + a placeholder assistant bubble (grown as
        // deltas stream in; replacement is keyed by id so concurrent history
        // edits stay safe). Bump the epoch and append under historyGate so a
        // background transcript load in flight — the cold-start topic
        // auto-select — can't overwrite them (see historyGate).
        val assistantId = Uuid.random().toString()
        historyGate.withLock {
            historyEpoch++
            _chatHistory.update { list ->
                val withUser = if (displayText.isNotEmpty()) {
                    list + History(role = History.Role.USER, content = displayText)
                } else {
                    list
                }
                withUser + History(id = assistantId, role = History.Role.ASSISTANT, content = "")
            }
        }
        val accumulated = StringBuilder()
        val replaceAssistant: (String, String?) -> Unit = { text, fallback ->
            _chatHistory.update { list ->
                list.map {
                    if (it.id == assistantId) it.copy(content = text, fallbackServiceName = fallback) else it
                }
            }
        }

        // Coalesce streaming updates. The gateway streams tokens faster than the
        // screen needs to repaint and faster than the 60-120Hz display; pushing every
        // token into _chatHistory re-runs the whole chat pipeline per token (the VM
        // combine, ChatScreen recomposition, the O(n) list copy in replaceAssistant,
        // the scroll-follow) and competes with scroll frames. Emit at most ~20/s — the
        // first token shows immediately, and the finalize block below always writes the
        // complete text, so nothing is dropped. Tuned toward smoothness over liveness:
        // the screen repaints in slightly larger chunks so scroll keeps more headroom.
        val streamEmitInterval = 50.milliseconds
        var lastStreamEmit = TimeSource.Monotonic.markNow()
        var streamEmitted = false
        val reply = try {
            sendStreaming(sendText) { delta ->
                accumulated.append(delta)
                if (!streamEmitted || lastStreamEmit.elapsedNow() >= streamEmitInterval) {
                    replaceAssistant(accumulated.toString(), null)
                    lastStreamEmit = TimeSource.Monotonic.markNow()
                    streamEmitted = true
                }
            }
        } catch (cancel: CancellationException) {
            throw cancel
        } catch (e: Exception) {
            // Older gateway without the stream endpoint, or a mid-stream failure.
            // Only retry through the blocking RPC when nothing streamed yet, so a
            // partial answer is never discarded or double-generated.
            if (accumulated.isEmpty()) {
                runCatching { send(sendText) }
                    .getOrElse { GatewayReply("⚠️ ${it.message ?: "gateway request failed"}") }
            } else {
                GatewayReply(text = accumulated.toString())
            }
        }

        // Finalize: pick text + fallback badge.
        //
        // `accumulated` holds every streamed delta — the full visible answer.
        // `reply.text` is the gateway terminal text (now BestText-corrected, but
        // keep this guard belt-and-suspenders): when the agent runs a tool
        // mid-answer (e.g. writing the reply to the wiki) the final turn is a
        // short wrap-up, so trusting it alone would erase the streamed body. Keep
        // the streamed accumulation when it's meaningfully longer than the
        // terminal text; otherwise use reply.text so the gateway's canonical
        // answer wins.
        val streamed = accumulated.toString()
        val finalText = when {
            streamed.length > reply.text.length + 40 -> streamed
            reply.text.isNotBlank() -> reply.text
            else -> streamed
        }
        replaceAssistant(
            finalText.ifBlank { "⚠️ 빈 응답" },
            // Mirrors the upstream fallback badge: show which model answered when the
            // gateway fell back from its main model to a fallback role.
            if (reply.fellBack && reply.model.isNotBlank()) reply.model else null,
        )
    }

    private fun formatCallback(submission: UiSubmission): String = buildString {
        append("[deneb-ui] event=").append(submission.pressedEvent)
        if (submission.values.isNotEmpty()) {
            append(" values={")
            append(submission.values.entries.joinToString(", ") { "${it.key}=${it.value}" })
            append("}")
        }
    }

    override fun clearHistory() {
        _chatHistory.value = emptyList()
    }

    // Drop the last user message and everything after it (its assistant reply).
    // The gateway client renders from its own [_chatHistory], so the base
    // implementation — which mutates RemoteDataRepository's separate flow — has
    // no visible effect here. Used by regenerate() before it re-asks.
    override fun popLastExchange() {
        _chatHistory.update { history ->
            val lastUserIndex = history.indexOfLast { it.role == History.Role.USER }
            if (lastUserIndex >= 0) history.take(lastUserIndex) else history
        }
    }

    override fun startNewChat() {
        _chatHistory.value = emptyList()
        // Scope the fresh conversation to a unique session off the client:main home.
        switchSession("client:main:${Uuid.random()}")
    }

    // --- Proactive-report deep link → session transcript --------------------

    /**
     * Fetch a session transcript and install it as the chat history UNLESS an
     * optimistic send (ask()) appended while we were fetching. Closes the
     * cold-start share race: the topic auto-select's transcript fetch used to
     * overwrite the just-shared message and its streaming reply, so the share
     * appeared to get no response until the next message was sent. Epoch-checked
     * under historyGate, so it is safe whichever of load / send finishes first.
     */
    private suspend fun loadTranscriptGuarded(key: String) {
        val startEpoch = historyGate.withLock { historyEpoch }
        val transcript = fetchTranscript(key)
        historyGate.withLock {
            if (historyEpoch == startEpoch) _chatHistory.value = transcript
        }
    }

    /**
     * Open the client:main home conversation where proactive reports are mirrored
     * — the deep-link target when the user taps a proactive-report push. Guarded so
     * a concurrent cold-start share can't be clobbered (see historyGate).
     */
    fun openWorkTopic() {
        switchSession("client:main")
        syncNativeStateAsync()
        scope.launch { loadTranscriptGuarded("client:main") }
    }

    /**
     * Cold-start home = the client:main 업무 topic, where proactive reports
     * (morning-letter, mail-analysis) are mirrored. Open it so those reports are
     * visible by default instead of an empty chat. base.restoreCurrentConversation
     * targets RemoteDataRepository's own history flow (not ours), so it has no
     * visible effect for the gateway client — this is the real restore.
     *
     * Guarded so a settings refresh (refreshSettings re-calls this) or a
     * cold-start share can't yank the user out of what they're viewing: only
     * open the home when nothing is loaded yet and we are still on the default
     * home session.
     */
    override fun restoreCurrentConversation() {
        if (_chatHistory.value.isEmpty() && sessionKey == "client:main") {
            openWorkTopic()
        }
    }

    /**
     * A proactive report just landed in client:main while the app is foregrounded
     * (so the scheduler raised no notification). If the user is already on the
     * home transcript, reload it so the report appears live — the SSE push frame
     * carries only a one-line preview, not the body. Otherwise fall back to the
     * base unread badge so the in-app banner points them at the work topic.
     */
    override fun onProactiveReportForeground() {
        syncNativeStateAsync()
        if (sessionKey == "client:main") {
            scope.launch { loadTranscriptGuarded("client:main") }
        } else {
            base.onProactiveReportForeground()
        }
    }

    fun refreshWorkFeedAsync() {
        scope.launch { refreshWorkFeed() }
    }

    fun syncNativeStateAsync() {
        scope.launch { syncNativeState() }
    }

    suspend fun syncNativeState(): Boolean {
        val reloadSessions = linkedSetOf<String>()
        var pulled = false
        var eventCount = 0
        nativeSyncGate.withLock {
            var cursor = nativeSyncCursor
            var keepGoing = true
            var pages = 0
            while (keepGoing && pages < 4) {
                val payload = callRpc<NativeSyncPayload>(
                    "miniapp.sync.pull",
                    buildJsonObject {
                        put("cursor", cursor)
                        put("limit", 100)
                    },
                ) ?: break
                pulled = true
                eventCount += payload.events.size
                payload.events.forEach { applyNativeSyncEvent(it, reloadSessions) }
                val nextCursor = payload.cursor.coerceAtLeast(cursor)
                if (nextCursor > nativeSyncCursor) {
                    nativeSyncCursor = nextCursor
                    appSettings.settings.putLong(KEY_SYNC_CURSOR, nextCursor)
                }
                keepGoing = payload.hasMore && nextCursor > cursor
                cursor = nextCursor
                pages++
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
        if (eventCount == 0 && _denebWorkFeed.value.isEmpty()) {
            refreshWorkFeed()
        }
        return true
    }

    suspend fun refreshWorkFeed(): Boolean {
        val payload = callRpc<WorkFeedPayload>(
            "miniapp.workfeed.list",
            buildJsonObject {
                put("limit", 20)
            },
        ) ?: return false
        _denebWorkFeed.value = payload.items.filter { it.id.isNotBlank() }
        return true
    }

    /** Observation plane (miniapp.observe.*): read the gateway's own behavior and
     *  recent logs for the settings 관찰 tab. Returns null on transport/auth failure. */
    internal suspend fun observeBehavior(days: Int): ObserveBehavior? =
        callRpc("miniapp.observe.behavior", buildJsonObject { put("days", days) })

    internal suspend fun observeLogs(level: String, limit: Int): ObserveLogsPayload? =
        callRpc("miniapp.observe.logs", buildJsonObject { put("level", level); put("limit", limit) })

    suspend fun openWorkFeedItem(id: String): String? {
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
        loadTranscriptGuarded(target)
        return prompt
    }

    // Dedicated side-conversation key for a 업무 card, in the same
    // client:main:<suffix> explicit-conversation namespace as startNewChat(). The id
    // is slugged to ascii so the key shape stays identical to client:main:<uuid>
    // (single colon-suffix); a blank id falls back to a random conversation.
    internal fun workItemSessionKey(itemId: String): String {
        val slug = itemId.trim().lowercase()
            .map { if (it in 'a'..'z' || it in '0'..'9') it else '-' }
            .joinToString("")
            .trim('-')
            .take(40)
        return if (slug.isEmpty()) "client:main:${Uuid.random()}" else "client:main:wf-$slug"
    }

    suspend fun runWorkFeedAction(itemId: String, actionId: String, adoptSession: Boolean = true): String? {
        if (itemId.isBlank() || actionId.isBlank()) return null
        val payload = callRpc<WorkFeedActionRunPayload>(
            "miniapp.workfeed.action.run",
            buildJsonObject {
                put("itemId", itemId)
                put("actionId", actionId)
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
            loadTranscriptGuarded(target)
        }
        return payload.prompt.ifBlank { null }
    }

    private fun applyNativeSyncEvent(event: NativeSyncEvent, reloadSessions: MutableSet<String>) {
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
        }
    }

    private fun decodeWorkFeedItem(payload: JsonObject?): WorkFeedItem? {
        val item = payload?.get("item") ?: return null
        return runCatching { jsonCodec.decodeFromJsonElement(WorkFeedItem.serializer(), item) }.getOrNull()
    }

    private fun decodeWorkFeedActionRun(payload: JsonObject?): NativeSyncActionPayload? =
        runCatching {
            payload?.let { jsonCodec.decodeFromJsonElement(NativeSyncActionPayload.serializer(), it) }
        }.getOrNull()

    private fun upsertSyncedWorkFeedItem(item: WorkFeedItem) {
        if (item.id.isBlank()) return
        if (item.status == "acked" || item.status == "snoozed") {
            _denebWorkFeed.update { items -> items.filterNot { it.id == item.id } }
            return
        }
        _denebWorkFeed.update { items ->
            val next = items.filterNot { it.id == item.id } + item
            next.sortedByDescending { it.createdAtMs }
        }
    }

    // Raise a durable proactive notification for a freshly-created work-feed item.
    // Called from applyNativeSyncEvent under nativeSyncGate, so the baseline read
    // and the cursor advance are serialized — each item notifies at most once.
    // Suppressed until the first sync has baselined (the catch-up over the closed
    // period must not barrage) and only for live unread items (acked/snoozed are
    // already dropped by upsertSyncedWorkFeedItem). tryEmit is non-blocking, so
    // holding the gate here is safe.
    private fun maybeEmitProactiveNotification(item: WorkFeedItem) {
        if (!nativeSyncBaselined) return
        if (item.id.isBlank() || item.status != "unread") return
        val body = item.summary.ifBlank { item.body }.ifBlank { item.title }
        _proactiveNotifications.tryEmit(
            ProactiveNotification(title = item.title.ifBlank { "Deneb" }, body = body),
        )
    }

    // --- Conversation drawer → Deneb sessions browser -----------------------
    // The drawer lists native topics first, then every recent Deneb session
    // (client, cron, system, legacy imports).
    // Tapping one loads its transcript AND repoints sessionKey at it, so the
    // next message continues that very conversation through the gateway.

    override fun loadConversations() {
        scope.launch {
            // Keep the current list when the fetch fails (null) so a transient
            // sessions.recent RPC error doesn't flap the drawer between the full
            // list and just the 업무 home row.
            val fresh = fetchRecentSessions() ?: return@launch
            _savedConversations.value = fresh
        }
    }

    override fun loadConversation(id: String) {
        switchSession(id)
        scope.launch { loadTranscriptGuarded(id) }
    }

    override suspend fun deleteConversation(id: String) {
        // Tell the gateway to drop the session — its in-memory entry AND its
        // transcript — then remove it from the local drawer list. The session
        // Manager is a pure in-memory map with no disk restore, so a local-only
        // removal resurrects on the next sessions.recent fetch (reopen the
        // drawer / restart the app). The server-side delete is what makes the
        // dismissal stick. A running session is refused server-side; it'll
        // reappear on the next fetch, which is correct (it's still live).
        callRpc<JsonObject>(
            "miniapp.sessions.delete",
            buildJsonObject { put("sessionKey", id) },
        )
        _savedConversations.update { list -> list.filterNot { it.id == id } }
    }

    // --- Memory screen → Deneb wiki (read-only browser) ---------------------
    // The wiki list RPC carries titles, not full bodies, so writing back from the
    // list view would clobber a page body with its title. Keep memory read-only
    // until a body-aware edit path exists; the value here is browsing Deneb's
    // knowledge base on the phone.

    override fun isMemoryEnabled(): Boolean = true

    override fun getMemories(): List<MemoryEntry> {
        scope.launch { refreshMemories() }
        return _denebMemories.value
    }

    override suspend fun updateMemoryContent(key: String, content: String) = Unit

    override suspend fun deleteMemory(key: String) = Unit

    // --- Scheduler screen → Deneb cron --------------------------------------

    override fun isSchedulingEnabled(): Boolean = true

    override fun getScheduledTasks(): List<ScheduledTask> {
        scope.launch { refreshScheduledTasks() }
        return _denebScheduledTasks.value
    }

    override suspend fun cancelScheduledTask(id: String) {
        removeCron(id)
    }

    suspend fun refreshClientStatus(): ClientStatus? {
        val payload = callRpc<ClientHelloPayload>("miniapp.client.hello", buildJsonObject {}) ?: run {
            _clientStatus.value = null
            return null
        }
        val status = ClientStatus(
            version = payload.version,
            nativeApiVersion = payload.nativeApiVersion,
            model = payload.model,
            capabilities = payload.capabilities,
            endpoints = payload.endpoints,
            timestampMs = payload.tsMs,
        )
        _clientStatus.value = status
        return status
    }

    /**
     * Check the gateway-served update manifest. The gateway exposes the APK +
     * metadata on its own port (the same base URL used for chat), so this works
     * over the cloudflare tunnel — unlike the old :19010 side-server the tunnel
     * never routed. Returns non-null only when a strictly newer build than the
     * compiled-in [DENEB_VERSION_CODE] is published.
     */
    suspend fun checkUpdate(): UpdateInfo? = runCatching {
        val base = gatewayUrl.trim().removeSuffix("/")
        if (base.isEmpty() || clientToken.isEmpty()) return@runCatching null
        val m = http.get("$base/api/v1/app/update/manifest") {
            header(CLIENT_TOKEN_HEADER, clientToken)
            // Bounded timeout: a missing or blocked gateway must fail fast
            // instead of hanging the "check for update" spinner forever.
            timeout {
                requestTimeoutMillis = 10_000
                connectTimeoutMillis = 6_000
            }
        }.body<UpdateManifest>()
        if (m.code > DENEB_VERSION_CODE && m.file.isNotBlank()) {
            // The browser opening this link can't set a header, so the client
            // token rides in the query string (same as the Gmail attachment route).
            val apk = "$base/api/v1/app/update/download" +
                "?file=${m.file.encodeURLParameter()}&clientToken=${clientToken.encodeURLParameter()}"
            UpdateInfo(buildLabel = m.code.toString(), apkUrl = apk, notes = m.notes)
        } else {
            null
        }
    }.getOrNull()

    private suspend fun send(message: String): GatewayReply {
        if (clientToken.isEmpty()) {
            return GatewayReply("⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요.")
        }
        val resp: RpcResponse = http.post("$gatewayUrl/api/v1/miniapp/rpc") {
            header(CLIENT_TOKEN_HEADER, clientToken)
            contentType(ContentType.Application.Json)
            setBody(
                RpcRequest(
                    id = Uuid.random().toString(),
                    method = "miniapp.chat.send",
                    params = SendParams(
                        message = message,
                        sessionKey = sessionKey,
                    ),
                ),
            )
        }.body()
        val payload = resp.payload
        return if (resp.ok && payload != null) {
            GatewayReply(text = payload.text, model = payload.model, fellBack = payload.fellBack)
        } else {
            GatewayReply("⚠️ 게이트웨이 오류")
        }
    }

    /**
     * Streaming counterpart of [send]: POSTs to the gateway's SSE chat endpoint
     * and invokes [onDelta] with each assistant text chunk as it arrives. The
     * terminal `done` frame carries the canonical text + which model answered,
     * returned as the [GatewayReply]. Throws on transport failure or a server
     * `error` frame so [ask] can fall back to the blocking RPC.
     *
     * SSE is parsed by hand off the response channel (no Ktor SSE plugin): lines
     * accumulate per frame and dispatch on the blank-line separator. Comment
     * lines (": keepalive") are ignored. The request timeout is disabled because
     * an agent turn (tool calls included) can outlast the default window.
     */
    private suspend fun sendStreaming(message: String, onDelta: (String) -> Unit): GatewayReply {
        if (clientToken.isEmpty()) {
            return GatewayReply("⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요.")
        }
        var model = ""
        var fellBack = false
        var doneText: String? = null
        http.preparePost("$gatewayUrl/api/v1/miniapp/chat/stream") {
            header(CLIENT_TOKEN_HEADER, clientToken)
            header("Accept", "text/event-stream")
            contentType(ContentType.Application.Json)
            setBody(SendParams(message = message, sessionKey = sessionKey))
            timeout {
                // No overall request cap: an agent turn (tool calls included) can
                // outlast any fixed window. Long.MAX_VALUE is the plugin's
                // "infinite" sentinel. The 15s server keepalive keeps the socket
                // from idling out within STREAM_SOCKET_TIMEOUT_MS.
                requestTimeoutMillis = Long.MAX_VALUE
                socketTimeoutMillis = STREAM_SOCKET_TIMEOUT_MS
            }
        }.execute { response ->
            if (!response.status.isSuccess()) {
                throw IllegalStateException("stream HTTP ${response.status.value}")
            }
            val channel = response.bodyAsChannel()
            var event = ""
            val data = StringBuilder()
            while (!channel.isClosedForRead) {
                val line = channel.readUTF8Line() ?: break
                when {
                    line.startsWith(":") -> Unit // comment / keepalive
                    line.startsWith("event:") -> event = line.removePrefix("event:").trim()
                    line.startsWith("data:") -> {
                        if (data.isNotEmpty()) data.append('\n')
                        data.append(line.removePrefix("data:").trimStart())
                    }
                    line.isEmpty() -> {
                        when (event) {
                            "delta" -> {
                                val d = runCatching {
                                    jsonCodec.decodeFromString(DeltaEvent.serializer(), data.toString()).delta
                                }.getOrNull()
                                if (!d.isNullOrEmpty()) onDelta(d)
                            }
                            "done" -> {
                                runCatching {
                                    jsonCodec.decodeFromString(DoneEvent.serializer(), data.toString())
                                }.getOrNull()?.let {
                                    doneText = it.text
                                    model = it.model
                                    fellBack = it.fellBack
                                }
                            }
                            "error" -> {
                                val msg = runCatching {
                                    jsonCodec.decodeFromString(ErrorEvent.serializer(), data.toString()).error
                                }.getOrNull() ?: "gateway stream error"
                                throw IllegalStateException(msg)
                            }
                        }
                        event = ""
                        data.clear()
                    }
                }
            }
        }
        return GatewayReply(text = doneText ?: "", model = model, fellBack = fellBack)
    }

    /**
     * Holds one long-lived SSE connection to the gateway's proactive-event
     * endpoint and invokes [onPush] for each {title, body} frame. Used by the
     * foreground daemon to raise a local notification the moment the gateway
     * produces a 업무-topic report (morning-letter, email-analysis), instead of
     * waiting for the next heartbeat poll.
     *
     * Reconnects with a small backoff after any drop (network change, server
     * restart, Android killing then restarting the daemon). Returns only when
     * the caller's coroutine is cancelled; missed frames while disconnected are
     * not replayed — the report is always also in the client:main transcript.
     */
    suspend fun subscribeEvents(onPush: (title: String, body: String) -> Unit) {
        var backoffMs = 2_000L
        while (currentCoroutineContext().isActive) {
            if (clientToken.isEmpty() || gatewayUrl.isBlank()) {
                delay(10_000)
                continue
            }
            try {
                http.prepareGet("$gatewayUrl/api/v1/miniapp/events") {
                    header(CLIENT_TOKEN_HEADER, clientToken)
                    header("Accept", "text/event-stream")
                    timeout {
                        // Long-lived: no overall cap. The 30s server keepalive
                        // keeps the socket under STREAM_SOCKET_TIMEOUT_MS.
                        requestTimeoutMillis = Long.MAX_VALUE
                        socketTimeoutMillis = STREAM_SOCKET_TIMEOUT_MS
                    }
                }.execute { response ->
                    if (!response.status.isSuccess()) {
                        throw IllegalStateException("events HTTP ${response.status.value}")
                    }
                    backoffMs = 2_000L // connected — reset backoff
                    syncNativeStateAsync()
                    val channel = response.bodyAsChannel()
                    var event = ""
                    val data = StringBuilder()
                    while (!channel.isClosedForRead) {
                        val line = channel.readUTF8Line() ?: break
                        when {
                            line.startsWith(":") -> Unit // keepalive comment
                            line.startsWith("event:") -> event = line.removePrefix("event:").trim()
                            line.startsWith("data:") -> {
                                if (data.isNotEmpty()) data.append('\n')
                                data.append(line.removePrefix("data:").trimStart())
                            }
                            line.isEmpty() -> {
                                if (event == "push") {
                                    runCatching {
                                        jsonCodec.decodeFromString(PushEvent.serializer(), data.toString())
                                    }.getOrNull()?.let { p ->
                                        if (p.body.isNotBlank()) {
                                            syncNativeStateAsync()
                                            onPush(p.title.ifBlank { "Deneb" }, p.body)
                                        }
                                    }
                                }
                                event = ""
                                data.clear()
                            }
                        }
                    }
                }
            } catch (cancel: CancellationException) {
                throw cancel
            } catch (_: Throwable) {
                // Drop/refused/timeout — back off and retry. Capped so a long
                // outage doesn't busy-loop but recovery stays reasonably quick.
                delay(backoffMs)
                backoffMs = (backoffMs * 2).coerceAtMost(60_000L)
            }
        }
    }

    private fun switchSession(key: String) {
        sessionKey = key
        _currentConversationId.value = key
    }

    private suspend fun fetchTranscript(sessionKey: String): List<History> {
        val payload = callRpc<TranscriptPayload>(
            "miniapp.sessions.transcript",
            buildJsonObject {
                put("sessionKey", sessionKey)
                put("limit", 200)
            },
        ) ?: return emptyList()
        return payload.messages.mapNotNull { m ->
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
                History(role = role, content = m.content, attachments = attachments)
            }
        }
    }

    /**
     * Generic POST to the miniapp RPC bridge. Returns the typed payload, or null
     * on any failure (missing token, transport error, non-ok response) so callers
     * degrade to empty rather than crash. Use this for non-critical reads; the
     * chat [send] keeps its own throwing path so the UI can surface errors.
     * Internal (not private) so the per-domain extension files can reach it.
     */
    internal suspend inline fun <reified T> callRpc(method: String, params: JsonObject): T? {
        if (clientToken.isEmpty()) return null
        return runCatching {
            http.post("$gatewayUrl/api/v1/miniapp/rpc") {
                header(CLIENT_TOKEN_HEADER, clientToken)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }.body<RpcEnv<T>>().payload
        }.getOrNull()
    }

    // rpcWrite posts a write RPC and surfaces the gateway's error message (so the
    // UI can show the exact reason), returning null on success. Internal (not
    // private) so the per-domain extension files can reach it.
    internal suspend fun rpcWrite(method: String, params: JsonObject): String? {
        if (clientToken.isEmpty()) return "게이트웨이에 연결되어 있지 않습니다."
        return runCatching {
            val result = http.post("$gatewayUrl/api/v1/miniapp/rpc") {
                header(CLIENT_TOKEN_HEADER, clientToken)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }.body<RpcResult>()
            if (result.ok) null else (result.error?.message?.ifBlank { null } ?: "요청을 처리하지 못했습니다.")
        }.getOrElse { "요청을 처리하지 못했습니다." }
    }

    @Serializable
    private data class RpcRequest(val id: String, val method: String, val params: SendParams)

    @Serializable
    private data class SendParams(
        val message: String,
        val sessionKey: String? = null,
    )

    @Serializable
    private data class RpcResponse(val ok: Boolean = false, val payload: SendPayload? = null)

    @Serializable
    private data class SendPayload(
        val text: String = "",
        val model: String = "",
        val sessionKey: String = "",
        // True when the gateway's model fallback chain fired (main → fallback);
        // `model` is then the model that actually answered. Surfaced as a badge.
        val fellBack: Boolean = false,
    )

    /** Internal result of one gateway chat turn (text + which model answered). */
    private data class GatewayReply(
        val text: String,
        val model: String = "",
        val fellBack: Boolean = false,
    )

    // SSE frame payloads from POST /api/v1/miniapp/chat/stream.
    @Serializable
    private data class DeltaEvent(val delta: String = "")

    @Serializable
    private data class DoneEvent(val text: String = "", val model: String = "", val fellBack: Boolean = false)

    @Serializable
    private data class ErrorEvent(val error: String = "")

    @Serializable
    private data class PushEvent(val title: String = "", val body: String = "")

    // RpcReq / RpcEnv are internal (not private) because the inline [callRpc]
    // references them from extension call sites.
    @Serializable
    internal data class RpcReq(val id: String, val method: String, val params: JsonObject)

    @Serializable
    internal data class RpcEnv<T>(val ok: Boolean = false, val payload: T? = null)

    // Error-bearing envelope for writes (e.g. calendar.create) where the caller
    // needs the gateway's error message, not just a null payload.
    @Serializable
    private data class RpcResult(val ok: Boolean = false, val error: RpcError? = null)

    @Serializable
    private data class RpcError(val code: String = "", val message: String = "")

    // Internal (not private) because the inline [callRpc] and the per-domain
    // extension files reference these constants.
    internal companion object {
        const val CLIENT_TOKEN_HEADER = "X-Deneb-Client-Token"
        const val KEY_URL = "deneb.gatewayUrl"
        const val KEY_TOKEN = "deneb.clientToken"
        const val KEY_SYNC_CURSOR = "deneb.nativeSyncCursor"

        // Android emulator → host loopback. On a real device set the gateway's
        // LAN/Tailscale URL under KEY_URL.
        const val DEFAULT_URL = "http://10.0.2.2:18789"
        const val REQUEST_TIMEOUT_MS = 180_000L

        // TCP connect budget. A reachable gateway on LAN/Tailscale connects well
        // under this; a dead one fails fast instead of waiting out REQUEST_TIMEOUT_MS.
        const val CONNECT_TIMEOUT_MS = 5_000L

        // Max idle between bytes on the chat SSE stream. The server emits a
        // keepalive comment every 15s, so this only trips on a real stall.
        const val STREAM_SOCKET_TIMEOUT_MS = 120_000L
    }
}
