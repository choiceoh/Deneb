package com.inspiredandroid.kai.deneb

import com.inspiredandroid.kai.data.AppSettings
import com.inspiredandroid.kai.data.Conversation
import com.inspiredandroid.kai.data.DataRepository
import com.inspiredandroid.kai.data.MemoryEntry
import com.inspiredandroid.kai.data.RemoteDataRepository
import com.inspiredandroid.kai.data.ScheduledTask
import com.inspiredandroid.kai.data.ServiceEntry
import com.inspiredandroid.kai.data.TaskStatus
import com.inspiredandroid.kai.data.TaskTrigger
import com.inspiredandroid.kai.data.UiSubmission
import com.inspiredandroid.kai.httpClient
import com.inspiredandroid.kai.contacts.ContactData
import com.inspiredandroid.kai.data.Attachment
import com.inspiredandroid.kai.deneb.generated.CalendarEventOut
import com.inspiredandroid.kai.deneb.generated.MailAnalysisOut
import com.inspiredandroid.kai.deneb.generated.MailMessageOut
import com.inspiredandroid.kai.deneb.generated.MiniappCronDetail
import com.inspiredandroid.kai.deneb.generated.QATurn
import com.inspiredandroid.kai.deneb.generated.SearchAllResult
import com.inspiredandroid.kai.deneb.generated.SessionRowOut
import com.inspiredandroid.kai.ui.chat.History
import kotlinx.collections.immutable.toImmutableList
import com.inspiredandroid.kai.ui.chat.WorkFeedItem
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_service_anthropic
import kai.composeapp.generated.resources.ic_service_deepseek
import kai.composeapp.generated.resources.ic_service_gemini
import kai.composeapp.generated.resources.ic_service_gemma
import kai.composeapp.generated.resources.ic_service_litert
import kai.composeapp.generated.resources.ic_service_longcat
import kai.composeapp.generated.resources.ic_service_minimax
import kai.composeapp.generated.resources.ic_service_mistral
import kai.composeapp.generated.resources.ic_service_mimo
import kai.composeapp.generated.resources.ic_service_moonshot
import kai.composeapp.generated.resources.ic_service_nvidia
import kai.composeapp.generated.resources.ic_service_openai
import kai.composeapp.generated.resources.ic_service_openai_compatible
import kai.composeapp.generated.resources.ic_service_qwen
import kai.composeapp.generated.resources.ic_service_step
import kai.composeapp.generated.resources.ic_service_xai
import kai.composeapp.generated.resources.ic_service_zai
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
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.TimeSource
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * A [DataRepository] backed by the Deneb gateway.
 *
 * It delegates every non-chat member to [base] (Kai's RemoteDataRepository, kept
 * so settings and the rest keep working) and overrides the chat path plus the
 * conversation drawer to drive the gateway's `miniapp.*` RPC surface. The reply
 * text may carry a ```kai-ui fence, which Kai's chat renderer turns into an
 * interactive screen.
 *
 * Auth uses the X-Deneb-Client-Token header. Generate the token on the gateway
 * host with `go run ./gateway-go/cmd/deneb-client-token` and set it, together
 * with the gateway URL, under the [KEY_URL] / [KEY_TOKEN] settings keys.
 *
 * Revival pattern: to bring another dead Kai screen back to life, override the
 * DataRepository method(s) it calls and route them through [callRpc]. Flow-typed
 * members (chatHistory, savedConversations) map cleanly; synchronous getters need
 * a cached StateFlow refreshed off [scope].
 */
@OptIn(ExperimentalUuidApi::class)
class DenebGatewayClient(
    private val base: RemoteDataRepository,
    private val appSettings: AppSettings,
) : DataRepository by base {

    private val jsonCodec = Json {
        ignoreUnknownKeys = true
        isLenient = true
    }

    private val http = httpClient {
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
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private val _chatHistory = MutableStateFlow<List<History>>(emptyList())
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

    // Deneb wiki pages surfaced through Kai's memory screen. getMemories() returns
    // this snapshot and also kicks a refresh; SettingsViewModel observes the flow
    // to rebuild its state once the RPC lands (see SettingsViewModel.init).
    private val _denebMemories = MutableStateFlow<List<MemoryEntry>>(emptyList())
    val denebMemories: StateFlow<List<MemoryEntry>> = _denebMemories

    // Deneb cron jobs surfaced through Kai's scheduler screen (same snapshot +
    // observe pattern as memory).
    private val _denebScheduledTasks = MutableStateFlow<List<ScheduledTask>>(emptyList())
    val denebScheduledTasks: StateFlow<List<ScheduledTask>> = _denebScheduledTasks

    // Deneb model registry, exposed to the config screen's model switcher.
    private val _denebModels = MutableStateFlow<List<ModelOption>>(emptyList())
    val denebModels: StateFlow<List<ModelOption>> = _denebModels

    // Current model id per role (main / lightweight / fallback) for the model tab.
    private val _denebRoleModels = MutableStateFlow<Map<String, String>>(emptyMap())
    val denebRoleModels: StateFlow<Map<String, String>> = _denebRoleModels

    // Recent Gmail surfaced in the native mail screen.
    private val _denebMail = MutableStateFlow<List<MailMessage>>(emptyList())
    val denebMail: StateFlow<List<MailMessage>> = _denebMail

    // Pagination cursor for the inbox; null when there are no more pages.
    private val _denebMailNextToken = MutableStateFlow<String?>(null)
    val denebMailNextToken: StateFlow<String?> = _denebMailNextToken

    // Upcoming calendar events surfaced in the native calendar screen.
    private val _denebCalendar = MutableStateFlow<List<CalendarEvent>>(emptyList())
    val denebCalendar: StateFlow<List<CalendarEvent>> = _denebCalendar

    // Native-client handshake snapshot: gateway version, active model, and
    // feature flags exposed by miniapp.client.hello.
    private val _clientStatus = MutableStateFlow<ClientStatus?>(null)
    val clientStatus: StateFlow<ClientStatus?> = _clientStatus

    // Native work feed: proactive reports and native shares as actionable rows.
    private val _denebWorkFeed = MutableStateFlow<List<WorkFeedItem>>(emptyList())
    val denebWorkFeed: StateFlow<List<WorkFeedItem>> = _denebWorkFeed

    private var sessionKey: String = "client:main"
    private val _currentConversationId = MutableStateFlow<String?>(sessionKey)
    override val currentConversationId: StateFlow<String?> = _currentConversationId

    private val gatewayUrl: String
        get() = appSettings.settings.getString(KEY_URL, DEFAULT_URL).trimEnd('/')

    private val clientToken: String
        get() = appSettings.settings.getString(KEY_TOKEN, "")

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?) {
        val displayText = question?.trim().orEmpty()
        // A kai-ui button press arrives as a UiSubmission. Show the friendly
        // question in the chat, but send the agent a structured callback naming
        // the event (per the kai-ui prompt contract) plus the collected inputs.
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
            // Mirrors Kai's fallback badge: show which model answered when the
            // gateway fell back from its main model to a fallback role.
            if (reply.fellBack && reply.model.isNotBlank()) reply.model else null,
        )
    }

    private fun formatCallback(submission: UiSubmission): String = buildString {
        append("[kai-ui] event=").append(submission.pressedEvent)
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
    private fun workItemSessionKey(itemId: String): String {
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
            "workfeed.created",
            "workfeed.updated" -> {
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

    private suspend fun refreshMemories() {
        val payload = callRpc<MemoryListPayload>(
            "miniapp.memory.list_in_category",
            buildJsonObject {
                put("category", "")
                put("limit", 200)
            },
        ) ?: return
        _denebMemories.value = payload.pages
            .filter { it.path.isNotBlank() }
            .map { p ->
                MemoryEntry(
                    key = p.path,
                    content = p.summary.ifBlank { p.title.ifBlank { p.path } },
                    createdAt = 0,
                    updatedAt = 0,
                )
            }
    }

    /** All wiki categories with page counts + corpus totals (`memory.categories`). */
    suspend fun fetchCategories(): WikiCategories? {
        val p = callRpc<CategoriesPayload>("miniapp.memory.categories", buildJsonObject {}) ?: return null
        return WikiCategories(
            categories = p.categories.map { WikiCategory(it.name, it.pageCount) },
            totalPages = p.totalPages,
            totalBytes = p.totalBytes,
        )
    }

    /** Pages within one category (`memory.list_in_category`); blank lists all. */
    /** Pages in a wiki category. Null on a fetch failure so the screen can offer
     *  retry instead of showing a misleading "empty category". */
    suspend fun fetchCategoryPages(category: String): List<WikiPageRef>? {
        val p = callRpc<MemoryListPayload>(
            "miniapp.memory.list_in_category",
            buildJsonObject {
                put("category", category)
                put("limit", 200)
            },
        ) ?: return null
        return p.pages
            .filter { it.path.isNotBlank() }
            .map { WikiPageRef(it.path, it.title.ifBlank { it.path }, it.summary, it.updated) }
    }

    /** Recent diary entries for the timeline (`miniapp.memory.diary_recent`).
     *  Null on a fetch failure so the screen can offer retry instead of showing
     *  a misleading empty timeline. */
    suspend fun fetchRecentDiary(limit: Int = 30): List<DiaryEntry>? {
        val p = callRpc<DiaryRecentPayload>(
            "miniapp.memory.diary_recent",
            buildJsonObject { put("limit", limit) },
        ) ?: return null
        return p.entries.map { DiaryEntry(header = it.header, content = it.content, file = it.file) }
    }

    /** Delete one or more wiki pages by path (`miniapp.memory.delete_pages`).
     *  The backend deletes best-effort and reports a per-page failure list, so
     *  this returns true only when every requested page was actually removed —
     *  letting the category screen surface a partial failure instead of
     *  silently dropping unselected rows. */
    suspend fun deleteCategoryPages(paths: List<String>): Boolean {
        if (paths.isEmpty()) return true
        val resp = callRpc<DeletePagesPayload>(
            "miniapp.memory.delete_pages",
            buildJsonObject {
                putJsonArray("paths") { paths.forEach { add(it) } }
            },
        ) ?: return false
        return resp.ok && resp.deleted == paths.size
    }

    // --- Scheduler screen → Deneb cron --------------------------------------

    override fun isSchedulingEnabled(): Boolean = true

    override fun getScheduledTasks(): List<ScheduledTask> {
        scope.launch { refreshScheduledTasks() }
        return _denebScheduledTasks.value
    }

    /** Suspend refresh that reports success, for screens that want an error state. */
    suspend fun loadScheduledTasks(): Boolean = refreshScheduledTasks()

    override suspend fun cancelScheduledTask(id: String) {
        removeCron(id)
    }

    /** Delete a cron, reporting success so the screen can confirm the delete landed
     *  before navigating away instead of popping back on a failed remove. */
    suspend fun removeCron(id: String): Boolean {
        val ok = callRpc<JsonObject>("miniapp.crons.remove", buildJsonObject { put("id", id) }) != null
        refreshScheduledTasks()
        return ok
    }

    private suspend fun refreshScheduledTasks(): Boolean {
        val payload = callRpc<CronListPayload>(
            "miniapp.crons.list",
            buildJsonObject { put("includeDisabled", true) },
        ) ?: return false
        _denebScheduledTasks.value = payload.jobs
            .filter { it.id.isNotBlank() }
            .map { j ->
                ScheduledTask(
                    id = j.id,
                    description = j.name.ifBlank { j.id },
                    prompt = j.payloadPreview,
                    scheduledAtEpochMs = j.nextRunAtMs,
                    createdAtEpochMs = 0,
                    cron = j.schedule.ifBlank { null },
                    trigger = TaskTrigger.CRON,
                    status = TaskStatus.PENDING,
                    lastResult = j.lastError.ifBlank { null },
                    consecutiveFailures = j.consecutiveErrors,
                )
            }
        return true
    }

    // --- Model switcher → Deneb registry ------------------------------------
    // models.set updates the gateway's default model, so switching here changes
    // chat across the native app and every gateway-run automation.

    fun refreshModelsAsync() {
        scope.launch { refreshModels() }
    }

    suspend fun refreshModels() {
        val payload = callRpc<ModelsPayload>("miniapp.models.list", buildJsonObject {}) ?: return
        _denebModels.value = payload.sections
            .flatMap { it.models }
            .distinctBy { it.id }
            .map { ModelOption(it.id, it.display.ifBlank { it.label.ifBlank { it.id } }, it.id == payload.current, it.health, it.custom) }
        _denebRoleModels.value = payload.roles.associate { it.role to it.model }
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

    suspend fun setMainModel(id: String): Boolean = setRoleModel(id, "main")

    /** Set the model for a specific role (main / lightweight / fallback). Returns
     *  false on a failed switch so the screen can surface it instead of a silent no-op. */
    suspend fun setRoleModel(id: String, role: String): Boolean {
        val ok = callRpc<JsonObject>(
            "miniapp.models.set",
            buildJsonObject {
                put("id", id)
                put("role", role)
            },
        ) != null
        refreshModels()
        return ok
    }

    /** Add an OpenAI-compatible model by base URL + model name. The gateway stores
     *  it as a custom provider (api=openai) and reloads live, so the model appears
     *  in [denebModels] after the refresh. Returns false when the gateway rejects
     *  the endpoint/model so the screen can surface it instead of a silent no-op. */
    suspend fun addCustomModel(endpoint: String, model: String): Boolean {
        val ok = callRpc<JsonObject>(
            "miniapp.models.add_custom",
            buildJsonObject {
                put("endpoint", endpoint)
                put("model", model)
            },
        ) != null
        if (ok) refreshModels()
        return ok
    }

    /** Remove a user-added custom model. The gateway resets any role bound to it
     *  back to the default. Returns false on failure. */
    suspend fun deleteCustomModel(id: String): Boolean {
        val ok = callRpc<JsonObject>(
            "miniapp.models.delete_custom",
            buildJsonObject {
                put("id", id)
            },
        ) != null
        if (ok) refreshModels()
        return ok
    }

    // --- Chat-input model switcher → Deneb registry --------------------------
    // Kai's chat input has a service/model switcher (ServiceSelector) driven by
    // ChatUiState.availableServices. When this client is active, ChatViewModel
    // sources that list from here so the switcher changes the gateway main model
    // instead of Kai's local providers.

    /** Gateway models as switcher entries, current model first (it renders as selected). */
    fun denebServiceEntries(): List<ServiceEntry> {
        val models = _denebModels.value
        val ordered = models.filter { it.current } + models.filterNot { it.current }
        return ordered.map { model ->
            ServiceEntry(
                instanceId = DENEB_MODEL_PREFIX + model.id,
                serviceId = "deneb",
                serviceName = model.display,
                modelId = model.id,
                icon = denebModelIcon(model),
            )
        }
    }

    /**
     * Best-effort brand icon for a gateway model. The gateway exposes no provider
     * field per model, so match well-known families on the id + display string.
     * Rendered monochrome (the switcher tints every icon), so these read as the
     * black-and-white brand marks rather than a single generic chip. Unknown or
     * local models fall back to the generic OpenAI-compatible mark.
     */
    private fun denebModelIcon(model: ModelOption) = with("${model.id} ${model.display}".lowercase()) {
        when {
            contains("claude") || contains("anthropic") -> Res.drawable.ic_service_anthropic
            contains("gemma") -> Res.drawable.ic_service_gemma
            contains("gemini") -> Res.drawable.ic_service_gemini
            contains("gpt") || contains("openai") || contains("chatgpt") ||
                contains("o1-") || contains("o3") || contains("o4") -> Res.drawable.ic_service_openai
            contains("deepseek") -> Res.drawable.ic_service_deepseek
            contains("kimi") || contains("moonshot") -> Res.drawable.ic_service_moonshot
            contains("mistral") || contains("mixtral") || contains("magistral") ||
                contains("ministral") || contains("codestral") || contains("devstral") -> Res.drawable.ic_service_mistral
            contains("grok") || contains("x-ai") || contains("xai") -> Res.drawable.ic_service_xai
            contains("glm") || contains("zai") || contains("z-ai") || contains("chatglm") -> Res.drawable.ic_service_zai
            contains("minimax") -> Res.drawable.ic_service_minimax
            contains("longcat") -> Res.drawable.ic_service_longcat
            contains("llama") || contains("nemotron") || contains("nvidia") -> Res.drawable.ic_service_nvidia
            contains("qwen") || contains("qwq") || contains("tongyi") -> Res.drawable.ic_service_qwen
            contains("mimo") || contains("xiaomi") -> Res.drawable.ic_service_mimo
            contains("step") || contains("stepfun") -> Res.drawable.ic_service_step
            // Local/on-device runtimes (vLLM-served small models) keep the edge mark.
            contains("litert") -> Res.drawable.ic_service_litert
            else -> Res.drawable.ic_service_openai_compatible
        }
    }

    /** Switch the gateway main model from a switcher tap (instanceId = prefixed model id). */
    fun selectDenebModelInstance(instanceId: String) {
        val modelId = instanceId.removePrefix(DENEB_MODEL_PREFIX)
        if (modelId.isBlank() || modelId == instanceId) return
        scope.launch { setMainModel(modelId) }
    }

    /** Refresh the recent inbox. Returns false on a fetch failure so the screen can
     *  show a retry instead of a misleading "no mail" empty state. */
    suspend fun refreshMail(): Boolean {
        val payload = callRpc<MailListPayload>(
            "miniapp.gmail.list_recent",
            buildJsonObject { put("limit", 25) },
        ) ?: return false
        _denebMail.value = payload.messages
            .filter { it.id.isNotBlank() }
            .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread) }
        _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
        return true
    }

    /** Append the next inbox page (if any) to the current list. */
    suspend fun loadMoreMail() {
        val token = _denebMailNextToken.value ?: return
        val payload = callRpc<MailListPayload>(
            "miniapp.gmail.list_recent",
            buildJsonObject {
                put("limit", 25)
                put("pageToken", token)
            },
        ) ?: return
        val seen = _denebMail.value.mapTo(HashSet()) { it.id }
        _denebMail.value = _denebMail.value + payload.messages
            .filter { it.id.isNotBlank() && it.id !in seen }
            .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread) }
        _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
    }

    suspend fun fetchMailDetail(id: String): MailDetail? {
        val row = callRpc<MailMessageOut>(
            "miniapp.gmail.get",
            buildJsonObject { put("id", id) },
        ) ?: return null
        return MailDetail(
            id = row.id,
            from = row.from,
            to = row.to,
            cc = row.cc,
            subject = row.subject,
            date = row.date,
            body = row.body,
            bodyTotal = row.bodyTotal,
            attachments = row.attachments
                .filter { it.id.isNotBlank() }
                .map { MailAttachment(it.id, it.filename.ifBlank { it.mimeType }, it.mimeType, it.size) },
        )
    }

    /** Mark read on the server and optimistically clear the unread dot in the list. */
    suspend fun markMailRead(id: String): Boolean {
        val ok = callRpc<OkPayload>("miniapp.gmail.mark_read", buildJsonObject { put("id", id) })?.ok == true
        if (ok) {
            _denebMail.update { list -> list.map { if (it.id == id) it.copy(unread = false) else it } }
        }
        return ok
    }

    /** Archive (drop from inbox); optimistically removes the row from the list. */
    suspend fun archiveMail(id: String): Boolean {
        val ok = callRpc<OkPayload>("miniapp.gmail.archive", buildJsonObject { put("id", id) })?.ok == true
        if (ok) _denebMail.update { list -> list.filterNot { it.id == id } }
        return ok
    }

    /** Move to Trash; optimistically removes the row from the list. */
    suspend fun trashMail(id: String): Boolean {
        val ok = callRpc<OkPayload>("miniapp.gmail.trash", buildJsonObject { put("id", id) })?.ok == true
        if (ok) _denebMail.update { list -> list.filterNot { it.id == id } }
        return ok
    }

    /** Instant cached analysis (no LLM call) if one was already produced on poll or earlier. */
    suspend fun fetchCachedAnalysis(id: String): MailAnalysis? =
        callRpc<MailAnalysisOut>("miniapp.gmail.analysis_cached", buildJsonObject { put("id", id) })?.toAnalysis()

    /** Run AI analysis; force=true reruns the LLM instead of returning the cached result. */
    suspend fun analyzeMail(id: String, force: Boolean = false): MailAnalysis? =
        callRpc<MailAnalysisOut>(
            "miniapp.gmail.analyze",
            buildJsonObject {
                put("id", id)
                if (force) put("force", true)
            },
        )?.toAnalysis()

    private fun MailAnalysisOut.toAnalysis(): MailAnalysis? =
        if (analysis.isBlank()) {
            null
        } else {
            MailAnalysis(
                text = analysis,
                related = relatedProjects.map { RelatedProject(it.path, it.title, it.summary) },
                cached = cached,
                createdAt = createdAt,
                durationMs = durationMs,
            )
        }

    /** Ask a follow-up about a message; prior Q&A is sent as history for multi-turn context. */
    suspend fun askMail(id: String, question: String, history: List<Pair<String, String>> = emptyList()): String? =
        callRpc<AskPayload>(
            "miniapp.gmail.ask",
            buildJsonObject {
                put("id", id)
                put("question", question)
                // History items use the generated QATurn wire shape (json q/a) so the
                // gateway's []QATurn binding actually receives them — the old hand-rolled
                // {question, answer} keys silently dropped all prior-turn context.
                putJsonArray("history") {
                    history.forEach { (q, a) -> add(jsonCodec.encodeToJsonElement(QATurn.serializer(), QATurn(q = q, a = a))) }
                }
            },
        )?.answer?.ifBlank { null }

    /**
     * Browser-openable attachment download URL. The download endpoint can't read
     * the X-Deneb-Client-Token header from a browser opening a link, so the token
     * rides in the query string (acceptable in this single-user local setup).
     */
    fun attachmentUrl(messageId: String, att: MailAttachment): String {
        fun e(s: String) = s.encodeURLParameter()
        return "$gatewayUrl/api/v1/miniapp/gmail/attachment" +
            "?messageId=${e(messageId)}&attachmentId=${e(att.id)}" +
            "&filename=${e(att.filename)}&mimeType=${e(att.mimeType)}&clientToken=${e(clientToken)}"
    }

    /** Fetch wiki / relationship context for a message's sender. */
    suspend fun fetchSenderContext(sender: String): SenderContext? {
        val p = callRpc<SenderContextPayload>(
            "miniapp.gmail.sender_context",
            buildJsonObject { put("sender", sender) },
        ) ?: return null
        return SenderContext(
            displayName = p.displayName.ifBlank { p.sender },
            email = p.email,
            recentCount = p.recent?.count ?: 0,
            windowDays = p.recent?.windowDays ?: 0,
            wikiHits = p.wikiHits.map { SenderWikiHit(it.title.ifBlank { it.path }, it.summary, it.category, it.path) },
            wikiFacts = p.wikiFacts,
        )
    }

    /** Recent messages from a specific sender (`list_recent` with a from: query). */
    suspend fun fetchRecentFromSender(email: String, limit: Int = 15): List<MailMessage> {
        if (email.isBlank()) return emptyList()
        val payload = callRpc<MailListPayload>(
            "miniapp.gmail.list_recent",
            buildJsonObject {
                put("query", "from:\"$email\"")
                put("limit", limit)
            },
        ) ?: return emptyList()
        return payload.messages
            .filter { it.id.isNotBlank() }
            .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread) }
    }

    /** Refresh upcoming events. Returns false on a fetch failure so the screen can
     *  tell a real "no events" from a network error instead of spinning forever. */
    suspend fun refreshCalendar(): Boolean {
        val payload = callRpc<CalListPayload>(
            "miniapp.calendar.list_upcoming",
            buildJsonObject {
                put("hoursAhead", 168) // one week ahead
                put("limit", 50)
            },
        ) ?: return false
        _denebCalendar.value = payload.events
            .filter { it.id.isNotBlank() }
            .map { CalendarEvent(it.id, it.summary, it.location, it.start, it.end, it.allDay, it.local) }
        return true
    }

    /**
     * Fetch events in an explicit [fromIso, toIso) window (`miniapp.calendar.list_range`).
     * The month grid uses this because it needs a whole month — often reaching into the
     * past — rather than [refreshCalendar]'s now-anchored look-ahead. Returns null on a
     * fetch failure so the screen can tell a real "no events" from a network error.
     */
    suspend fun fetchCalendarRange(fromIso: String, toIso: String): List<CalendarEvent>? {
        val payload = callRpc<CalListPayload>(
            "miniapp.calendar.list_range",
            buildJsonObject {
                put("from", fromIso)
                put("to", toIso)
            },
        ) ?: return null
        return payload.events
            .filter { it.id.isNotBlank() }
            .map { CalendarEvent(it.id, it.summary, it.location, it.start, it.end, it.allDay, it.local) }
    }

    /**
     * Create a calendar event by hand (`miniapp.calendar.create`). The gateway
     * stores it locally, so this always works without a Google write scope.
     * Returns null on success, or a Korean error message on failure. start/end
     * are RFC3339; pass end blank to let the gateway apply a default duration.
     */
    suspend fun createCalendarEvent(
        summary: String,
        description: String,
        location: String,
        allDay: Boolean,
        startIso: String,
        endIso: String,
        timeZone: String,
    ): String? = rpcWrite(
        "miniapp.calendar.create",
        calendarWriteParams(summary, description, location, allDay, startIso, endIso, timeZone),
    )

    /** Edit a locally-stored event (`miniapp.calendar.update`). Same return contract
     *  as [createCalendarEvent]; the gateway rejects non-local (Google) IDs. */
    suspend fun updateCalendarEvent(
        id: String,
        summary: String,
        description: String,
        location: String,
        allDay: Boolean,
        startIso: String,
        endIso: String,
        timeZone: String,
    ): String? = rpcWrite(
        "miniapp.calendar.update",
        calendarWriteParams(summary, description, location, allDay, startIso, endIso, timeZone, id),
    )

    /** Delete a locally-stored event (`miniapp.calendar.delete`). Null on success,
     *  a Korean error message otherwise (e.g. when the id is a read-only Google event). */
    suspend fun deleteCalendarEvent(id: String): String? =
        rpcWrite("miniapp.calendar.delete", buildJsonObject { put("id", id) })

    // calendarWriteParams builds the shared create/update body; `id` is set only
    // for updates. Blank optional fields are omitted so the gateway applies defaults.
    private fun calendarWriteParams(
        summary: String,
        description: String,
        location: String,
        allDay: Boolean,
        startIso: String,
        endIso: String,
        timeZone: String,
        id: String? = null,
    ): JsonObject = buildJsonObject {
        if (id != null) put("id", id)
        put("summary", summary)
        if (description.isNotBlank()) put("description", description)
        if (location.isNotBlank()) put("location", location)
        put("allDay", allDay)
        put("start", startIso)
        if (endIso.isNotBlank()) put("end", endIso)
        if (timeZone.isNotBlank()) put("timeZone", timeZone)
    }

    // rpcWrite posts a write RPC and surfaces the gateway's error message (so the
    // UI can show the exact reason), returning null on success.
    private suspend fun rpcWrite(method: String, params: JsonObject): String? {
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

    /**
     * One-shot glanceable summary for the home-screen widget: the next upcoming
     * event and the unread-mail count. Returns a not-configured summary when the
     * gateway token is unset, and ok=false on a fetch error so the widget shows a
     * quiet fallback instead of stale data.
     */
    suspend fun widgetSummary(): WidgetSummary {
        if (clientToken.isEmpty() || gatewayUrl.isBlank()) {
            return WidgetSummary(configured = false)
        }
        return runCatching {
            // Calendar and mail are independent — fetch them concurrently so the
            // widget refresh costs one RTT instead of the sum of two.
            coroutineScope {
                val calDeferred = async {
                    callRpc<CalListPayload>(
                        "miniapp.calendar.list_upcoming",
                        buildJsonObject {
                            put("hoursAhead", 168)
                            put("limit", 5)
                        },
                    )
                }
                val mailDeferred = async {
                    callRpc<MailListPayload>(
                        "miniapp.gmail.list_recent",
                        buildJsonObject { put("limit", 25) },
                    )
                }
                val cal = calDeferred.await()
                val mail = mailDeferred.await()
                val next = cal?.events?.firstOrNull { it.id.isNotBlank() }
                val meeting = next?.let { formatMeeting(it.summary, it.start, it.allDay) }.orEmpty()
                val msgs = mail?.messages.orEmpty()
                val unread = msgs.count { it.isUnread }
                // The most recent message (read or unread) as a one-line glance.
                val latestMail = msgs.firstOrNull { it.id.isNotBlank() }
                    ?.let { mailGlance(it.from, it.subject) }.orEmpty()
                WidgetSummary(meeting = meeting, unread = unread, latestMail = latestMail)
            }
        }.getOrElse { WidgetSummary(ok = false) }
    }

    // mailGlance renders "sender · subject" for the widget's recent-mail line.
    // Sender is the display name before any <email>; subject falls back to a
    // placeholder so the line is never just a bare name.
    private fun mailGlance(from: String, subject: String): String {
        val lt = from.indexOf('<')
        val name = (if (lt > 0) from.take(lt) else from).trim().trim('"').ifBlank { from.trim() }
        val subj = subject.trim().ifBlank { "(제목 없음)" }
        return "$name · $subj"
    }

    // formatMeeting renders "M/D HH:mm · title" from an RFC3339 start using only
    // string ops, to keep this widget hot-path free of a date-library dependency.
    private fun formatMeeting(title: String, start: String, allDay: Boolean): String {
        val t = title.trim().ifBlank { "일정" }
        val md = runCatching {
            val parts = start.take(10).split("-") // 2026-05-31
            "${parts[1].toInt()}/${parts[2].toInt()}"
        }.getOrDefault("")
        val hm = if (!allDay && start.length >= 16 && start[10] == 'T') start.substring(11, 16) else ""
        val whenStr = listOf(md, hm).filter { it.isNotBlank() }.joinToString(" ")
        return if (whenStr.isBlank()) t else "$whenStr · $t"
    }

    /** Full calendar event (attendees, Meet link, description) for the detail screen. */
    suspend fun fetchCalendarEvent(id: String): CalendarEventDetail? {
        val p = callRpc<CalendarEventOut>(
            "miniapp.calendar.get",
            buildJsonObject { put("id", id) },
        ) ?: return null
        return CalendarEventDetail(
            id = p.id,
            title = p.summary,
            description = p.description,
            location = p.location,
            start = p.start,
            end = p.end,
            allDay = p.allDay,
            organizer = p.organizer?.let { it.displayName.ifBlank { it.email } }.orEmpty(),
            attendees = p.attendees.mapNotNull { (it.displayName.ifBlank { it.email }).ifBlank { null } },
            status = p.status,
            local = p.local,
        )
    }

    /** Unified search across wiki, diary and people (`miniapp.search.all`). */
    suspend fun searchAll(query: String): SearchResults? {
        val p = callRpc<SearchAllResult>(
            "miniapp.search.all",
            buildJsonObject {
                put("query", query)
                put("limit", 20)
            },
        ) ?: return null
        return SearchResults(
            wiki = p.wiki.filter { it.path.isNotBlank() }
                .map { SearchHit(it.path, it.title.ifBlank { it.path }, it.snippet.ifBlank { it.summary }, it.category) },
            diary = p.diary.map { SearchHit("", it.header.ifBlank { "일기" }, it.content, "diary") },
            people = p.people.filter { it.email.isNotBlank() || it.name.isNotBlank() }
                .map { PersonHit(it.name.ifBlank { it.email }, it.email, it.messageCount, it.lastSubject) },
        )
    }

    /** Topic doc files (`miniapp.topicdocs.list_files`). */
    /** Topic doc files. Null on a fetch failure so the tab can offer retry. */
    suspend fun fetchTopicDocs(): List<TopicDocFile>? {
        val p = callRpc<TopicDocsListPayload>("miniapp.topicdocs.list_files", buildJsonObject {}) ?: return null
        return p.files.filter { it.name.isNotBlank() }.map { TopicDocFile(it.name, it.modified) }
    }

    /** Read one topic doc (`miniapp.topicdocs.read_file`). */
    suspend fun readTopicDoc(name: String): TopicDocContent? {
        val p = callRpc<TopicDocReadPayload>(
            "miniapp.topicdocs.read_file",
            buildJsonObject { put("name", name) },
        ) ?: return null
        return TopicDocContent(p.name.ifBlank { name }, p.content, p.modified)
    }

    /** People ranked by recent message volume (`miniapp.people.list`). Null on a
     *  fetch failure so the screen can offer retry instead of a misleading "empty". */
    suspend fun fetchPeople(): List<PersonHit>? {
        val p = callRpc<PeopleListPayload>(
            "miniapp.people.list",
            buildJsonObject { put("limit", 60) },
        ) ?: return null
        return p.people
            .filter { it.email.isNotBlank() || it.name.isNotBlank() }
            .map { PersonHit(it.name.ifBlank { it.email }, it.email, it.messageCount, it.lastSubject) }
    }

    /** Full wiki/memory page by path (`miniapp.memory.get_page`). */
    suspend fun fetchWikiPage(path: String): WikiPage? {
        val p = callRpc<WikiPagePayload>(
            "miniapp.memory.get_page",
            buildJsonObject { put("path", path) },
        ) ?: return null
        return WikiPage(
            path = p.path,
            title = p.title.ifBlank { p.path },
            summary = p.summary,
            category = p.category,
            tags = p.tags,
            updated = p.updated,
            body = p.body,
        )
    }

    /** Overwrite a wiki page; non-null title/summary/tags also update frontmatter. */
    suspend fun saveWikiPage(
        path: String,
        body: String,
        title: String? = null,
        summary: String? = null,
        tags: List<String>? = null,
    ): Boolean =
        callRpc<JsonObject>(
            "miniapp.memory.write_page",
            buildJsonObject {
                put("path", path)
                put("body", body)
                if (title != null) put("title", title)
                if (summary != null) put("summary", summary)
                if (tags != null) putJsonArray("tags") { tags.forEach { add(it) } }
            },
        ) != null

    /** Create a new wiki page (`miniapp.memory.create_page`); returns its path. */
    suspend fun createWikiPage(title: String, category: String, body: String): String? =
        callRpc<WikiPagePayload>(
            "miniapp.memory.create_page",
            buildJsonObject {
                put("title", title)
                put("category", category)
                put("body", body)
            },
        )?.path

    /** Write (or create) a topic doc (`miniapp.topicdocs.write_file`). */
    suspend fun saveTopicDoc(name: String, content: String, create: Boolean): Boolean =
        callRpc<JsonObject>(
            "miniapp.topicdocs.write_file",
            buildJsonObject {
                put("name", name)
                put("content", content)
                put("create", create)
            },
        ) != null

    /** Trigger a cron job immediately (`miniapp.crons.run`). */
    suspend fun runCron(id: String): Boolean =
        callRpc<JsonObject>("miniapp.crons.run", buildJsonObject { put("id", id) }) != null

    /** Full cron job detail (`miniapp.crons.get`). */
    suspend fun fetchCron(id: String): CronDetail? {
        val p = callRpc<MiniappCronDetail>("miniapp.crons.get", buildJsonObject { put("id", id) }) ?: return null
        return CronDetail(
            id = p.id,
            name = p.name,
            enabled = p.enabled,
            schedule = p.schedule,
            scheduleSpec = p.scheduleSpec,
            scheduleKind = p.scheduleKind,
            timezone = p.timezone,
            payloadKind = p.payloadKind,
            prompt = p.prompt,
            model = p.model,
            deliveryChannel = p.deliveryChannel,
            deliveryTo = p.deliveryTo,
            nextRunAtMs = p.nextRunAtMs,
            lastDeliveryStatus = p.lastDeliveryStatus,
            lastError = p.lastError,
            consecutiveErrors = p.consecutiveErrors,
            autoDisabledAtMs = p.autoDisabledAtMs,
        )
    }

    /** Enable or disable a cron job (`miniapp.crons.update`). */
    suspend fun setCronEnabled(id: String, enabled: Boolean): Boolean =
        callRpc<JsonObject>(
            "miniapp.crons.update",
            buildJsonObject { put("id", id); put("enabled", enabled) },
        ) != null

    /**
     * Patch an existing cron job (`miniapp.crons.update`). Only the arguments the
     * caller passes non-null are sent; each maps to the gateway's optional-pointer
     * patch, so omitted fields stay untouched (editing the schedule alone never
     * blanks the prompt). The gateway parses the schedule spec and returns its
     * reason on a bad expression — surfaced here so the edit form can show it.
     * Returns null on success, an error message otherwise. Refreshes the cached
     * task list on success so the list row reflects the edit.
     */
    suspend fun updateCron(
        id: String,
        name: String? = null,
        schedule: String? = null,
        tz: String? = null,
        prompt: String? = null,
        model: String? = null,
    ): String? {
        val err = rpcWrite(
            "miniapp.crons.update",
            buildJsonObject {
                put("id", id)
                if (name != null) put("name", name)
                if (schedule != null) put("schedule", schedule)
                if (tz != null) put("tz", tz)
                if (prompt != null) put("prompt", prompt)
                if (model != null) put("model", model)
            },
        )
        if (err == null) refreshScheduledTasks()
        return err
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

    /**
     * OCR a shared image on the gateway and run one agent turn over the extracted
     * text, showing the result in the chat. The native client's "share an image to
     * Deneb" path — the gateway uses the PaddleOCR sidecar (tesseract fallback).
     */
    @OptIn(ExperimentalEncodingApi::class)
    suspend fun captureImage(bytes: ByteArray, mimeType: String, caption: String = ""): Boolean {
        if (clientToken.isEmpty() || bytes.isEmpty()) return false
        val trimmedCaption = caption.trim()
        val label = if (trimmedCaption.isNotBlank()) {
            trimmedCaption + "\n📷 이미지 OCR 분석 중…"
        } else {
            "📷 이미지 공유됨 (OCR 분석 중…)"
        }
        _chatHistory.update { it + History(role = History.Role.USER, content = label) }
        val reply = runCatching {
            val payload = callRpc<CaptureImagePayload>(
                "miniapp.capture.image",
                buildJsonObject {
                    put("image", Base64.encode(bytes))
                    put("mimeType", mimeType)
                    put("sessionKey", sessionKey)
                    // Source context the image alone lacks (originating app/sender/
                    // notification text); the gateway prepends it to the OCR turn.
                    if (trimmedCaption.isNotBlank()) put("caption", trimmedCaption)
                },
            )
            payload?.text?.ifBlank { null } ?: "이미지에서 텍스트를 찾지 못했거나 분석에 실패했습니다."
        }.getOrElse { "⚠️ ${it.message ?: "이미지 캡처 실패"}" }
        _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
        syncNativeStateAsync()
        return true
    }

    /**
     * Transcribe a shared audio recording (voice memo, meeting audio) via the
     * gateway's VibeVoice-ASR sidecar and run one agent turn over the diarized
     * transcript (speaker labels + timestamps). This is the native client's
     * "share a recording to Deneb" path.
     */
    @OptIn(ExperimentalEncodingApi::class)
    suspend fun captureAudio(bytes: ByteArray, mimeType: String) {
        if (clientToken.isEmpty() || bytes.isEmpty()) return
        _chatHistory.update { it + History(role = History.Role.USER, content = "🎙️ 녹음 공유됨 (전사·회의록 분석 중…)") }
        val reply = runCatching {
            val payload = callRpc<CaptureAudioPayload>(
                "miniapp.capture.audio",
                buildJsonObject {
                    put("audio", Base64.encode(bytes))
                    put("mimeType", mimeType)
                    put("sessionKey", sessionKey)
                },
            )
            payload?.text?.ifBlank { null } ?: "녹음에서 음성을 인식하지 못했거나 전사에 실패했습니다."
        }.getOrElse { "⚠️ ${it.message ?: "녹음 캡처 실패"}" }
        _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
        syncNativeStateAsync()
    }

    /**
     * Sync the device address book into the gateway. The gateway enriches ONLY the
     * people already in its wiki (it creates no pages) with phone/email/org — so a
     * sync both sharpens ASR proper-noun bias and powers "whose number is this?"
     * lookups, without uploading the whole phone book as new entries. Runs one
     * gateway turn and shows the Korean summary in the chat transcript.
     */
    suspend fun captureContacts(contacts: List<ContactData>) {
        if (clientToken.isEmpty() || contacts.isEmpty()) return
        _chatHistory.update { it + History(role = History.Role.USER, content = "📇 주소록 ${contacts.size}개 동기화 중…") }
        val reply = runCatching {
            val payload = callRpc<CaptureContactsPayload>(
                "miniapp.capture.contacts",
                buildJsonObject {
                    putJsonArray("contacts") {
                        contacts.forEach { contact ->
                            addJsonObject {
                                put("name", contact.name)
                                putJsonArray("phones") { contact.phones.forEach { add(it) } }
                                putJsonArray("emails") { contact.emails.forEach { add(it) } }
                                put("org", contact.org)
                            }
                        }
                    }
                    put("sessionKey", sessionKey)
                },
            )
            payload?.text?.ifBlank { null } ?: "주소록 동기화에 실패했습니다."
        }.getOrElse { "⚠️ ${it.message ?: "주소록 동기화 실패"}" }
        _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
        syncNativeStateAsync()
    }

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

    // The right-side drawer is a pure session browser. It used to synthesize the
    // configured topics (업무/잡담/코딩 from deneb.json topics.map) as pinned fake
    // conversations at the top, but the topic switcher UI is gone and the client
    // is a single client:main session model now — so that synthesis only leaked
    // the retired topics back into the drawer. List real Deneb sessions only, and
    // fall back to a lone client:main home when there are no sessions yet so the
    // drawer is never empty.
    private suspend fun fetchRecentSessions(): List<Conversation>? {
        // null return = RPC failed (timeout/transient/load). The caller keeps the
        // existing drawer list instead of collapsing to just the home row.
        val payload = callRpc<RecentPayload>(
            "miniapp.sessions.recent",
            buildJsonObject { put("limit", 50) },
        ) ?: return null
        val recent = payload.sessions
            ?.filter { it.key.isNotBlank() }
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
        // Always pin the client:main 업무 home to the top. If it's already in the
        // recent list (it usually is), lift it there; otherwise synthesize it. The
        // home used to appear only via the empty-list fallback, so once ANY other
        // session existed the main 업무 conversation dropped out of the drawer —
        // which is exactly why it looked "missing".
        val home = recent.find { it.id == "client:main" }
            ?: Conversation(
                id = "client:main",
                messages = emptyList(),
                createdAt = 0,
                updatedAt = kotlin.time.Clock.System.now().toEpochMilliseconds(),
                title = "업무",
            )
        return listOf(home) + recent.filterNot { it.id == "client:main" }
    }

    private fun conversationTitle(s: SessionRowOut): String {
        if (s.label.isNotBlank()) return s.label
        // The single home session keeps the familiar 업무 label (matches the
        // empty-drawer fallback), not "내 대화 · main".
        if (s.key == "client:main") return "업무"
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
     */
    private suspend inline fun <reified T> callRpc(method: String, params: JsonObject): T? {
        if (clientToken.isEmpty()) return null
        return runCatching {
            http.post("$gatewayUrl/api/v1/miniapp/rpc") {
                header(CLIENT_TOKEN_HEADER, clientToken)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }.body<RpcEnv<T>>().payload
        }.getOrNull()
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

    @Serializable
    private data class RpcReq(val id: String, val method: String, val params: JsonObject)

    @Serializable
    private data class RpcEnv<T>(val ok: Boolean = false, val payload: T? = null)

    // Error-bearing envelope for writes (e.g. calendar.create) where the caller
    // needs the gateway's error message, not just a null payload.
    @Serializable
    private data class RpcResult(val ok: Boolean = false, val error: RpcError? = null)

    @Serializable
    private data class RpcError(val code: String = "", val message: String = "")

    private companion object {
        const val CLIENT_TOKEN_HEADER = "X-Deneb-Client-Token"
        const val DENEB_MODEL_PREFIX = "deneb-model:"
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
