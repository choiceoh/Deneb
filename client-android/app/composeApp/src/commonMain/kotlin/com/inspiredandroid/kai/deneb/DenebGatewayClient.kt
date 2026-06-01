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
import com.inspiredandroid.kai.ui.chat.History
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_service_anthropic
import kai.composeapp.generated.resources.ic_service_deepseek
import kai.composeapp.generated.resources.ic_service_gemini
import kai.composeapp.generated.resources.ic_service_litert
import kai.composeapp.generated.resources.ic_service_longcat
import kai.composeapp.generated.resources.ic_service_minimax
import kai.composeapp.generated.resources.ic_service_mistral
import kai.composeapp.generated.resources.ic_service_moonshot
import kai.composeapp.generated.resources.ic_service_nvidia
import kai.composeapp.generated.resources.ic_service_openai
import kai.composeapp.generated.resources.ic_service_openai_compatible
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
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
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
        install(HttpTimeout) { requestTimeoutMillis = REQUEST_TIMEOUT_MS }
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

    private val _savedConversations = MutableStateFlow<List<Conversation>>(emptyList())
    override val savedConversations: StateFlow<List<Conversation>> = _savedConversations

    // Per-topic knowledge topics from the gateway (deneb.json topics.map),
    // mirroring the Telegram forum topics. Drives the chat-screen topic switcher;
    // empty when topics are unconfigured, in which case the switcher is hidden
    // and chat falls back to the single "client:main" session.
    private val _topics = MutableStateFlow<List<DenebTopic>>(emptyList())
    val topics: StateFlow<List<DenebTopic>> = _topics

    // The currently selected topic key, echoed back to the gateway on each send
    // so per-topic knowledge injection fires exactly as it does on Telegram.
    // null = no topic (legacy single-session chat).
    private val _selectedTopic = MutableStateFlow<String?>(null)
    val selectedTopic: StateFlow<String?> = _selectedTopic

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

    private var sessionKey: String = "client:main"

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

        val reply = try {
            sendStreaming(sendText) { delta ->
                accumulated.append(delta)
                replaceAssistant(accumulated.toString(), null)
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
        // Scope the fresh conversation to the active topic so its transcript and
        // per-topic knowledge stay separate from other topics.
        val base = topicSessionKey(_selectedTopic.value)
        sessionKey = "$base:${Uuid.random()}"
    }

    // --- Topic switcher → per-topic knowledge + sessions --------------------
    // Topics mirror the Telegram forum topics (deneb.json topics.map). Selecting
    // one repoints sessionKey at that topic's thread and echoes the key on every
    // send, so the gateway injects the same <key>.md knowledge it would on
    // Telegram (see miniapp.chat.send topicKey).

    /** Fetch the configured topics and default to General (threadId "0"). */
    fun loadTopics() {
        scope.launch { refreshTopics() }
    }

    /**
     * Loads the configured topics, defaults to General, and returns them. The
     * suspend form lets the share flow await the list before showing its topic
     * picker — a cold share has no chat open to have loaded them yet.
     */
    suspend fun refreshTopics(): List<DenebTopic> {
        val payload = callRpc<TopicsListPayload>("miniapp.topics.list", buildJsonObject {})
        val topics = payload?.topics
            ?.filter { it.key.isNotBlank() }
            ?.map { DenebTopic(key = it.key, threadId = it.threadId) }
            ?: emptyList()
        _topics.value = topics
        // Only auto-select when a General (forum threadId "0", i.e. 업무)
        // topic is configured — that maps to the legacy "client:main" home,
        // so the default view is unchanged. Without a General topic the
        // named topics stay opt-in and chat stays on "client:main", so a
        // returning user keeps their existing conversation on first open.
        if (_selectedTopic.value == null) {
            topics.firstOrNull { it.threadId == "0" }?.let { selectTopic(it.key) }
        }
        return topics
    }

    /** Switch the active topic: repoint the session and load its transcript. */
    fun selectTopic(topicKey: String) {
        if (_selectedTopic.value == topicKey) return
        _selectedTopic.value = topicKey
        val key = topicSessionKey(topicKey)
        sessionKey = key
        scope.launch { loadTranscriptGuarded(key) }
    }

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
     * Open the 업무 (General) topic where proactive reports are mirrored — the
     * deep-link target when the user taps a proactive-report push. Selects the
     * General topic (threadId "0") when topics are configured; otherwise loads
     * the legacy "client:main" session directly so the report is visible even
     * without a topic map.
     */
    fun openWorkTopic() {
        val general = _topics.value.firstOrNull { it.threadId == "0" }
        if (general != null) {
            selectTopic(general.key)
        } else {
            sessionKey = "client:main"
            scope.launch { _chatHistory.value = fetchTranscript("client:main") }
        }
    }

    // General (forum threadId "0", i.e. 업무) keeps the legacy "client:main"
    // session so existing history carries over; named topics get their own.
    private fun topicSessionKey(topicKey: String?): String {
        if (topicKey.isNullOrBlank()) return "client:main"
        val t = _topics.value.firstOrNull { it.key == topicKey }
        return if (t != null && t.threadId == "0") "client:main" else "client:topic:$topicKey"
    }

    // --- Conversation drawer → Deneb sessions browser -----------------------
    // The drawer lists every recent Deneb session (Telegram, cron, client …).
    // Tapping one loads its transcript AND repoints sessionKey at it, so the
    // next message continues that very conversation through the gateway.

    override fun loadConversations() {
        scope.launch { _savedConversations.value = fetchRecentSessions() }
    }

    override fun loadConversation(id: String) {
        sessionKey = id
        scope.launch { loadTranscriptGuarded(id) }
    }

    override suspend fun deleteConversation(id: String) {
        // Deneb sessions have no delete RPC; drop it from the local view only.
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
    suspend fun fetchCategoryPages(category: String): List<WikiPageRef> {
        val p = callRpc<MemoryListPayload>(
            "miniapp.memory.list_in_category",
            buildJsonObject {
                put("category", category)
                put("limit", 200)
            },
        ) ?: return emptyList()
        return p.pages
            .filter { it.path.isNotBlank() }
            .map { WikiPageRef(it.path, it.title.ifBlank { it.path }, it.summary, it.updated) }
    }

    // --- Scheduler screen → Deneb cron --------------------------------------

    override fun isSchedulingEnabled(): Boolean = true

    override fun getScheduledTasks(): List<ScheduledTask> {
        scope.launch { refreshScheduledTasks() }
        return _denebScheduledTasks.value
    }

    override suspend fun cancelScheduledTask(id: String) {
        callRpc<JsonObject>("miniapp.crons.remove", buildJsonObject { put("id", id) })
        refreshScheduledTasks()
    }

    private suspend fun refreshScheduledTasks() {
        val payload = callRpc<CronListPayload>(
            "miniapp.crons.list",
            buildJsonObject { put("includeDisabled", true) },
        ) ?: return
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
                    lastResult = j.lastError,
                    consecutiveFailures = j.consecutiveErrors ?: 0,
                )
            }
    }

    // --- Model switcher → Deneb registry ------------------------------------
    // models.set updates the gateway's default model, so switching here changes
    // chat across every Deneb surface (Telegram, Mini App, this client).

    fun refreshModelsAsync() {
        scope.launch { refreshModels() }
    }

    suspend fun refreshModels() {
        val payload = callRpc<ModelsPayload>("miniapp.models.list", buildJsonObject {}) ?: return
        _denebModels.value = payload.sections
            .flatMap { it.models }
            .distinctBy { it.id }
            .map { ModelOption(it.id, it.display.ifBlank { it.label.ifBlank { it.id } }, it.id == payload.current, it.health) }
        _denebRoleModels.value = payload.roles.associate { it.role to it.model }
    }

    suspend fun setMainModel(id: String) = setRoleModel(id, "main")

    /** Set the model for a specific role (main / lightweight / fallback). */
    suspend fun setRoleModel(id: String, role: String) {
        callRpc<JsonObject>(
            "miniapp.models.set",
            buildJsonObject {
                put("id", id)
                put("role", role)
            },
        )
        refreshModels()
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
            contains("gemini") || contains("gemma") -> Res.drawable.ic_service_gemini
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

    suspend fun refreshMail() {
        val payload = callRpc<MailListPayload>(
            "miniapp.gmail.list_recent",
            buildJsonObject { put("limit", 25) },
        ) ?: return
        _denebMail.value = payload.messages
            .filter { it.id.isNotBlank() }
            .map { MailMessage(it.id, it.from, it.subject, it.snippet, it.date, it.isUnread) }
        _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
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
        val row = callRpc<MailDetailRow>(
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
        callRpc<AnalyzePayload>("miniapp.gmail.analysis_cached", buildJsonObject { put("id", id) })?.toAnalysis()

    /** Run AI analysis; force=true reruns the LLM instead of returning the cached result. */
    suspend fun analyzeMail(id: String, force: Boolean = false): MailAnalysis? =
        callRpc<AnalyzePayload>(
            "miniapp.gmail.analyze",
            buildJsonObject {
                put("id", id)
                if (force) put("force", true)
            },
        )?.toAnalysis()

    private fun AnalyzePayload.toAnalysis(): MailAnalysis? =
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
                putJsonArray("history") {
                    history.forEach { (q, a) ->
                        addJsonObject {
                            put("question", q)
                            put("answer", a)
                        }
                    }
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

    suspend fun refreshCalendar() {
        val payload = callRpc<CalListPayload>(
            "miniapp.calendar.list_upcoming",
            buildJsonObject {
                put("hoursAhead", 168) // one week ahead
                put("limit", 50)
            },
        ) ?: return
        _denebCalendar.value = payload.events
            .filter { it.id.isNotBlank() }
            .map { CalendarEvent(it.id, it.summary, it.location, it.start, it.end, it.allDay, it.hasMeet) }
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
            val cal = callRpc<CalListPayload>(
                "miniapp.calendar.list_upcoming",
                buildJsonObject {
                    put("hoursAhead", 168)
                    put("limit", 5)
                },
            )
            val next = cal?.events?.firstOrNull { it.id.isNotBlank() }
            val meeting = next?.let { formatMeeting(it.summary, it.start, it.allDay) }.orEmpty()

            val mail = callRpc<MailListPayload>(
                "miniapp.gmail.list_recent",
                buildJsonObject { put("limit", 25) },
            )
            val unread = mail?.messages?.count { it.isUnread } ?: 0

            WidgetSummary(meeting = meeting, unread = unread)
        }.getOrElse { WidgetSummary(ok = false) }
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
        val p = callRpc<CalEventPayload>(
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
            meetUri = p.conference?.uri.orEmpty(),
            status = p.status,
        )
    }

    /** Unified search across wiki, diary and people (`miniapp.search.all`). */
    suspend fun searchAll(query: String): SearchResults? {
        val p = callRpc<SearchPayload>(
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
    suspend fun fetchTopicDocs(): List<TopicDocFile> {
        val p = callRpc<TopicDocsListPayload>("miniapp.topicdocs.list_files", buildJsonObject {}) ?: return emptyList()
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

    /** People ranked by recent message volume (`miniapp.people.list`). */
    suspend fun fetchPeople(): List<PersonHit> {
        val p = callRpc<PeopleListPayload>(
            "miniapp.people.list",
            buildJsonObject { put("limit", 60) },
        ) ?: return emptyList()
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
        val p = callRpc<CronDetailPayload>("miniapp.crons.get", buildJsonObject { put("id", id) }) ?: return null
        return CronDetail(
            id = p.id,
            name = p.name,
            enabled = p.enabled,
            schedule = p.schedule,
            scheduleSpec = p.scheduleSpec,
            scheduleKind = p.scheduleKind,
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

    @Serializable
    private data class CronDetailPayload(
        val id: String = "",
        val name: String = "",
        val enabled: Boolean = false,
        val schedule: String = "",
        val scheduleSpec: String = "",
        val scheduleKind: String = "",
        val payloadKind: String = "",
        val prompt: String = "",
        val model: String = "",
        val deliveryChannel: String = "",
        val deliveryTo: String = "",
        val nextRunAtMs: Long = 0,
        val lastDeliveryStatus: String = "",
        val lastError: String = "",
        val consecutiveErrors: Int = 0,
        val autoDisabledAtMs: Long = 0,
    )

    /** APK + version.json are served on :19010 of the same host as the gateway. */
    private val updateBaseUrl: String
        get() {
            val u = gatewayUrl.trim().removeSuffix("/")
            val schemeEnd = u.indexOf("://").let { if (it >= 0) it + 3 else 0 }
            val scheme = if (schemeEnd > 0) u.substring(0, schemeEnd) else "http://"
            val host = u.substring(schemeEnd).substringBefore("/").substringBefore(":")
            return "$scheme$host:19010"
        }

    /**
     * Check the self-served manifest. Returns non-null only when a strictly
     * newer build than the compiled-in [DENEB_VERSION_CODE] is published.
     */
    suspend fun checkUpdate(): UpdateInfo? = runCatching {
        val m = http.get("$updateBaseUrl/version.json").body<UpdateManifest>()
        if (m.code > DENEB_VERSION_CODE && m.url.isNotBlank()) {
            UpdateInfo(versionName = m.name.ifBlank { m.code.toString() }, apkUrl = m.url, notes = m.notes)
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
    suspend fun captureImage(bytes: ByteArray, mimeType: String, caption: String = "") {
        if (clientToken.isEmpty() || bytes.isEmpty()) return
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
    }

    @Serializable
    private data class CaptureImagePayload(val text: String = "")

    /**
     * Transcribe a shared audio recording (voice memo, meeting audio) via the
     * gateway's VibeVoice-ASR sidecar and run one agent turn over the diarized
     * transcript (speaker labels + timestamps). The native client's "share a
     * recording to Deneb" path — capture the Telegram bot can't do on Android.
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
    }

    @Serializable
    private data class CaptureAudioPayload(val text: String = "")

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
                        topicKey = _selectedTopic.value,
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
                                        if (p.body.isNotBlank()) onPush(p.title.ifBlank { "Deneb" }, p.body)
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

    private suspend fun fetchRecentSessions(): List<Conversation> {
        val payload = callRpc<RecentPayload>(
            "miniapp.sessions.recent",
            buildJsonObject { put("limit", 50) },
        ) ?: return emptyList()
        return payload.sessions
            .filter { it.key.isNotBlank() }
            .map { s ->
                Conversation(
                    id = s.key,
                    messages = emptyList(),
                    createdAt = if (s.startedAtMs > 0) s.startedAtMs else s.updatedAtMs,
                    updatedAt = s.updatedAtMs,
                    title = conversationTitle(s),
                )
            }
    }

    private fun conversationTitle(s: SessionRow): String {
        if (s.label.isNotBlank()) return s.label
        val friendly = when (s.key.substringBefore(':', "")) {
            "telegram" -> "텔레그램"
            "client" -> "내 대화"
            "system" -> "시스템"
            "cron" -> "예약 작업"
            else -> "대화"
        }
        val shortId = s.key.substringAfterLast(':').take(8)
        return if (shortId.isNotBlank()) "$friendly · $shortId" else friendly
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
            if (m.content.isBlank()) null else History(role = role, content = m.content)
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
        // Per-topic knowledge selector (see miniapp.topics.list); null/omitted
        // means no per-topic injection.
        val topicKey: String? = null,
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

    @Serializable
    private data class RecentPayload(val sessions: List<SessionRow> = emptyList())

    @Serializable
    private data class SessionRow(
        val key: String = "",
        val label: String = "",
        val channel: String = "",
        val kind: String = "",
        val updatedAtMs: Long = 0,
        val startedAtMs: Long = 0,
    )

    @Serializable
    private data class TranscriptPayload(val messages: List<TranscriptMsg> = emptyList())

    @Serializable
    private data class TranscriptMsg(val role: String = "", val content: String = "", val timestampMs: Long = 0)

    @Serializable
    private data class MemoryListPayload(val pages: List<MemoryPageRow> = emptyList())

    @Serializable
    private data class MemoryPageRow(
        val path: String = "",
        val title: String = "",
        val summary: String = "",
        val updated: String = "",
    )

    @Serializable
    private data class MemoryCategoryRow(val name: String = "", val pageCount: Int = 0)

    @Serializable
    private data class CategoriesPayload(
        val categories: List<MemoryCategoryRow> = emptyList(),
        val totalPages: Int = 0,
        val totalBytes: Long = 0,
    )

    @Serializable
    private data class CronListPayload(val jobs: List<CronRow> = emptyList())

    @Serializable
    private data class CronRow(
        val id: String = "",
        val name: String = "",
        val enabled: Boolean = true,
        val schedule: String = "",
        val payloadPreview: String = "",
        val nextRunAtMs: Long = 0,
        val consecutiveErrors: Int? = null,
        val lastError: String? = null,
    )

    @Serializable
    private data class ModelsPayload(
        val current: String = "",
        val roles: List<RoleModelRow> = emptyList(),
        val sections: List<ModelSection> = emptyList(),
    )

    @Serializable
    private data class RoleModelRow(val role: String = "", val model: String = "")

    @Serializable
    private data class ModelSection(val title: String = "", val models: List<ModelRow> = emptyList())

    @Serializable
    private data class ModelRow(
        val id: String = "",
        val display: String = "",
        val label: String = "",
        val current: Boolean = false,
        val health: String = "",
    )

    @Serializable
    private data class MailListPayload(
        val messages: List<MailRow> = emptyList(),
        val nextPageToken: String = "",
    )

    @Serializable
    private data class MailRow(
        val id: String = "",
        val from: String = "",
        val subject: String = "",
        val snippet: String = "",
        val date: String = "",
        val isUnread: Boolean = false,
    )

    @Serializable
    private data class MailDetailRow(
        val id: String = "",
        val from: String = "",
        val to: String = "",
        val cc: String = "",
        val subject: String = "",
        val date: String = "",
        val body: String = "",
        val bodyTotal: Int = 0,
        val attachments: List<MailAttachmentRow> = emptyList(),
    )

    @Serializable
    private data class MailAttachmentRow(
        val id: String = "",
        val filename: String = "",
        val mimeType: String = "",
        val size: Int = 0,
    )

    @Serializable
    private data class OkPayload(val ok: Boolean = false)

    @Serializable
    private data class ProjectRefRow(val path: String = "", val title: String = "", val summary: String = "")

    @Serializable
    private data class AnalyzePayload(
        val analysis: String = "",
        val relatedProjects: List<ProjectRefRow> = emptyList(),
        val cached: Boolean = false,
        val createdAt: String = "",
        val durationMs: Long = 0,
    )

    @Serializable
    private data class AskPayload(val answer: String = "")

    @Serializable
    private data class SenderContextPayload(
        val sender: String = "",
        val email: String = "",
        val displayName: String = "",
        val recent: MailSenderRecent? = null,
        val wikiHits: List<MailWikiHit> = emptyList(),
        val wikiFacts: String = "",
    )

    @Serializable
    private data class MailSenderRecent(val count: Int = 0, val lastReceivedAt: String = "", val windowDays: Int = 0)

    @Serializable
    private data class MailWikiHit(
        val path: String = "",
        val title: String = "",
        val summary: String = "",
        val category: String = "",
    )

    @Serializable
    private data class CalListPayload(val events: List<CalRow> = emptyList())

    @Serializable
    private data class CalRow(
        val id: String = "",
        val summary: String = "",
        val location: String = "",
        val start: String = "",
        val end: String = "",
        val allDay: Boolean = false,
        val hasMeet: Boolean = false,
    )

    @Serializable
    private data class CalEventPayload(
        val id: String = "",
        val summary: String = "",
        val description: String = "",
        val location: String = "",
        val start: String = "",
        val end: String = "",
        val allDay: Boolean = false,
        val organizer: CalAttendee? = null,
        val attendees: List<CalAttendee> = emptyList(),
        val conference: CalConference? = null,
        val htmlLink: String = "",
        val status: String = "",
    )

    @Serializable
    private data class CalAttendee(val email: String = "", val displayName: String = "", val responseStatus: String = "")

    @Serializable
    private data class CalConference(val solution: String = "", val uri: String = "")

    @Serializable
    private data class SearchPayload(
        val wiki: List<SearchWikiRow> = emptyList(),
        val diary: List<SearchDiaryRow> = emptyList(),
        val people: List<SearchPersonRow> = emptyList(),
    )

    @Serializable
    private data class SearchWikiRow(
        val path: String = "",
        val title: String = "",
        val summary: String = "",
        val category: String = "",
        val snippet: String = "",
    )

    @Serializable
    private data class SearchDiaryRow(val file: String = "", val header: String = "", val content: String = "")

    @Serializable
    private data class SearchPersonRow(
        val email: String = "",
        val name: String = "",
        val messageCount: Int = 0,
        val lastSubject: String = "",
    )

    @Serializable
    private data class PeopleListPayload(val people: List<SearchPersonRow> = emptyList())

    @Serializable
    private data class TopicDocsListPayload(val files: List<TopicDocRow> = emptyList())

    @Serializable
    private data class TopicDocRow(val name: String = "", val size: Long = 0, val modified: String = "")

    @Serializable
    private data class TopicDocReadPayload(val name: String = "", val content: String = "", val modified: String = "")

    @Serializable
    private data class TopicsListPayload(val topics: List<TopicRow> = emptyList())

    @Serializable
    private data class TopicRow(val key: String = "", val threadId: String = "")

    @Serializable
    private data class WikiPagePayload(
        val path: String = "",
        val title: String = "",
        val summary: String = "",
        val category: String = "",
        val tags: List<String> = emptyList(),
        val related: List<String> = emptyList(),
        val updated: String = "",
        val body: String = "",
    )

    private companion object {
        const val CLIENT_TOKEN_HEADER = "X-Deneb-Client-Token"
        const val DENEB_MODEL_PREFIX = "deneb-model:"
        const val KEY_URL = "deneb.gatewayUrl"
        const val KEY_TOKEN = "deneb.clientToken"

        // Android emulator → host loopback. On a real device set the gateway's
        // LAN/Tailscale URL under KEY_URL.
        const val DEFAULT_URL = "http://10.0.2.2:18789"
        const val REQUEST_TIMEOUT_MS = 180_000L

        // Max idle between bytes on the chat SSE stream. The server emits a
        // keepalive comment every 15s, so this only trips on a real stall.
        const val STREAM_SOCKET_TIMEOUT_MS = 120_000L
    }
}

/** A switchable Deneb model surfaced in the config screen's model picker. */
data class ModelOption(
    val id: String,
    val display: String,
    val current: Boolean,
    val health: String,
)

/** A recent Gmail message shown in the native mail screen. */
data class MailMessage(
    val id: String,
    val from: String,
    val subject: String,
    val snippet: String,
    val date: String,
    val unread: Boolean,
)

/** Full Gmail message for the native detail screen. */
data class MailDetail(
    val id: String,
    val from: String,
    val to: String,
    val cc: String,
    val subject: String,
    val date: String,
    val body: String,
    val bodyTotal: Int,
    val attachments: List<MailAttachment>,
)

/** A downloadable attachment on a message (id + size kept for download). */
data class MailAttachment(
    val id: String,
    val filename: String,
    val mimeType: String,
    val size: Int,
)

/** A wiki project page cited by an analysis, surfaced as a tappable chip. */
data class RelatedProject(val path: String, val title: String, val summary: String)

/** Result of an AI mail analysis (fresh or cached). */
data class MailAnalysis(
    val text: String,
    val related: List<RelatedProject> = emptyList(),
    val cached: Boolean = false,
    val createdAt: String = "",
    val durationMs: Long = 0,
)

/** Sender relationship context (recent volume + cited wiki pages). */
data class SenderContext(
    val displayName: String,
    val email: String,
    val recentCount: Int,
    val windowDays: Int,
    val wikiHits: List<SenderWikiHit>,
    val wikiFacts: String,
)

data class SenderWikiHit(val title: String, val summary: String, val category: String, val path: String = "")

/** Glanceable home-widget data: next-meeting line + unread-mail count. */
data class WidgetSummary(
    val meeting: String = "",
    val unread: Int = 0,
    val configured: Boolean = true,
    val ok: Boolean = true,
)

/** An upcoming calendar event shown in the native calendar screen. */
data class CalendarEvent(
    val id: String,
    val title: String,
    val location: String,
    val start: String,
    val end: String,
    val allDay: Boolean,
    val hasMeet: Boolean,
)

/** Full calendar event for the detail screen. */
data class CalendarEventDetail(
    val id: String,
    val title: String,
    val description: String,
    val location: String,
    val start: String,
    val end: String,
    val allDay: Boolean,
    val organizer: String,
    val attendees: List<String>,
    val meetUri: String,
    val status: String,
)

/** Full cron job detail for the cron screen (`miniapp.crons.get`). */
data class CronDetail(
    val id: String,
    val name: String,
    val enabled: Boolean,
    val schedule: String,
    val scheduleSpec: String,
    val scheduleKind: String,
    val payloadKind: String,
    val prompt: String,
    val model: String,
    val deliveryChannel: String,
    val deliveryTo: String,
    val nextRunAtMs: Long,
    val lastDeliveryStatus: String,
    val lastError: String,
    val consecutiveErrors: Int,
    val autoDisabledAtMs: Long,
)

/** Unified search results across wiki, diary and people. */
data class SearchResults(
    val wiki: List<SearchHit>,
    val diary: List<SearchHit>,
    val people: List<PersonHit>,
)

data class SearchHit(val path: String, val title: String, val snippet: String, val category: String)

data class PersonHit(val name: String, val email: String, val messageCount: Int, val lastSubject: String)

/** A topic doc file in the hub list. */
data class TopicDocFile(val name: String, val modified: String)

/**
 * One per-topic knowledge topic (deneb.json topics.map), rendered as a switch
 * in the chat top bar. [key] is the topic key (also the button label and the
 * value echoed to the gateway on send); [threadId] is the Telegram forum thread
 * it maps to, with "0" marking the General (업무) topic.
 */
data class DenebTopic(val key: String, val threadId: String)

/** A topic doc's content for the read view. */
data class TopicDocContent(val name: String, val content: String, val modified: String)

/** Full wiki/memory page for the page view. */
data class WikiPage(
    val path: String,
    val title: String,
    val summary: String,
    val category: String,
    val tags: List<String>,
    val updated: String,
    val body: String,
)

/** A wiki category with its page count, for the category browser. */
data class WikiCategory(val name: String, val pageCount: Int)

/** All wiki categories plus corpus totals. */
data class WikiCategories(
    val categories: List<WikiCategory>,
    val totalPages: Int,
    val totalBytes: Long,
)

/** A page reference within a category listing (tap -> wiki page). */
data class WikiPageRef(
    val path: String,
    val title: String,
    val summary: String,
    val updated: String,
)
