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
import kai.composeapp.generated.resources.ic_refresh
import io.github.vinceglb.filekit.PlatformFile
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.serialization.kotlinx.json.json
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
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
        if (displayText.isNotEmpty()) {
            _chatHistory.update { it + History(role = History.Role.USER, content = displayText) }
        }
        val reply = runCatching { send(sendText) }
            .getOrElse { "⚠️ ${it.message ?: "gateway request failed"}" }
        _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
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

    override fun startNewChat() {
        _chatHistory.value = emptyList()
        sessionKey = "client:${Uuid.random()}"
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
        scope.launch { _chatHistory.value = fetchTranscript(id) }
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
    }

    suspend fun setMainModel(id: String) {
        callRpc<JsonObject>(
            "miniapp.models.set",
            buildJsonObject {
                put("id", id)
                put("role", "main")
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
                icon = Res.drawable.ic_refresh,
            )
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
            attachments = row.attachments.map { it.filename.ifBlank { it.mimeType } },
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

    /** Run (or fetch cached) AI analysis for a message; returns the analysis text. */
    suspend fun analyzeMail(id: String): String? =
        callRpc<AnalyzePayload>("miniapp.gmail.analyze", buildJsonObject { put("id", id) })
            ?.analysis?.ifBlank { null }

    /** Ask a free-form question about a message; returns the answer text. */
    suspend fun askMail(id: String, question: String): String? =
        callRpc<AskPayload>(
            "miniapp.gmail.ask",
            buildJsonObject {
                put("id", id)
                put("question", question)
            },
        )?.answer?.ifBlank { null }

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
            wikiHits = p.wikiHits.map { SenderWikiHit(it.title.ifBlank { it.path }, it.summary, it.category) },
            wikiFacts = p.wikiFacts,
        )
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

    private suspend fun send(message: String): String {
        if (clientToken.isEmpty()) {
            return "⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요."
        }
        val resp: RpcResponse = http.post("$gatewayUrl/api/v1/miniapp/rpc") {
            header(CLIENT_TOKEN_HEADER, clientToken)
            contentType(ContentType.Application.Json)
            setBody(
                RpcRequest(
                    id = Uuid.random().toString(),
                    method = "miniapp.chat.send",
                    params = SendParams(message = message, sessionKey = sessionKey),
                ),
            )
        }.body()
        return if (resp.ok && resp.payload != null) resp.payload.text else "⚠️ 게이트웨이 오류"
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
    private data class SendParams(val message: String, val sessionKey: String? = null)

    @Serializable
    private data class RpcResponse(val ok: Boolean = false, val payload: SendPayload? = null)

    @Serializable
    private data class SendPayload(val text: String = "", val model: String = "", val sessionKey: String = "")

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
    private data class ModelsPayload(val current: String = "", val sections: List<ModelSection> = emptyList())

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
    private data class AnalyzePayload(val analysis: String = "", val cached: Boolean = false)

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

    private companion object {
        const val CLIENT_TOKEN_HEADER = "X-Deneb-Client-Token"
        const val DENEB_MODEL_PREFIX = "deneb-model:"
        const val KEY_URL = "deneb.gatewayUrl"
        const val KEY_TOKEN = "deneb.clientToken"

        // Android emulator → host loopback. On a real device set the gateway's
        // LAN/Tailscale URL under KEY_URL.
        const val DEFAULT_URL = "http://10.0.2.2:18789"
        const val REQUEST_TIMEOUT_MS = 180_000L
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
    val attachments: List<String>,
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

data class SenderWikiHit(val title: String, val summary: String, val category: String)

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
