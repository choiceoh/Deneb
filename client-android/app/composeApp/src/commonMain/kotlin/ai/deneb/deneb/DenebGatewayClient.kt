package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.Attachment
import ai.deneb.data.Conversation
import ai.deneb.data.DataRepository
import ai.deneb.data.FallbackStatus
import ai.deneb.data.MemoryEntry
import ai.deneb.data.ScheduledTask
import ai.deneb.data.SmsDraft
import ai.deneb.data.SmsDraftStatus
import ai.deneb.data.SmsDraftStore
import ai.deneb.data.UiSubmission
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.httpClient
import ai.deneb.sms.SmsSendResult
import ai.deneb.sms.SmsSender
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.WorkFeedItem
import io.github.vinceglb.filekit.PlatformFile
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.prepareGet
import io.ktor.client.request.preparePost
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsChannel
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import io.ktor.utils.io.readUTF8Line
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.put
import kotlin.concurrent.Volatile
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.Duration.Companion.seconds
import kotlin.time.TimeSource
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * A [DataRepository] backed by the Deneb gateway — the sole production implementation.
 *
 * It implements the full (narrow) [DataRepository] surface directly and drives the
 * gateway's `miniapp.*` RPC surface for the chat path plus the conversation drawer.
 * The reply text may carry a ```deneb-ui fence, which the chat renderer turns into an
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
 * The remaining non-chat [DataRepository] members the UI still reaches through the
 * interface (recall read, SMS drafts, file extensions, the heartbeat / work-report
 * notification pulses) are implemented inline in the "DataRepository: non-chat
 * surface" section near the bottom. The legacy on-device (cloud-direct) provider
 * path that used to back those via `RemoteDataRepository` delegation is gone.
 */
@OptIn(ExperimentalUuidApi::class)
class DenebGatewayClient(
    // internal (not private) so session-list extensions can read the active
    // workspace (recall on = 업무, off = 챗봇) to filter the recent-session list.
    internal val appSettings: AppSettings,
    private val smsDraftStore: SmsDraftStore,
    private val smsSender: SmsSender,
) : DataRepository {

    internal val jsonCodec = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
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

    /** Whether the main model accepts image input (from miniapp.models.list). When
     *  true the model picker hides the opt-in 비전 role — a separate vision model is
     *  redundant since images route to the main model directly. */
    internal val _denebMainHasVision = MutableStateFlow(false)
    val denebMainHasVision: StateFlow<Boolean> = _denebMainHasVision

    internal val _denebSkills = MutableStateFlow<List<SkillRow>>(emptyList())
    val denebSkills: StateFlow<List<SkillRow>> = _denebSkills

    // Recent mail surfaced in the native mail screen.
    internal val _denebMail = MutableStateFlow<List<MailMessage>>(emptyList())
    val denebMail: StateFlow<List<MailMessage>> = _denebMail

    internal val _denebMailNativeStatus = MutableStateFlow<MailNativeStatus?>(null)
    val denebMailNativeStatus: StateFlow<MailNativeStatus?> = _denebMailNativeStatus

    // Pagination cursor for the inbox; null when there are no more pages.
    internal val _denebMailNextToken = MutableStateFlow<String?>(null)
    val denebMailNextToken: StateFlow<String?> = _denebMailNextToken

    // Mail query behind the current mail list (null = default inbox view).
    // Set by refreshMail on success; loadMoreMail must send the same query or
    // the next page would come from a different result set than the cursor.
    internal var denebMailActiveQuery: String? = null

    // Ids read this session, kept so a list refetch can't resurrect the unread
    // dot. markMailRead clears it in _denebMail optimistically, but on phone a
    // back-nav recomposes the list and re-runs refreshMail inside the gateway's
    // 30s list cache — which still reports the mail unread (mark_read deliberately
    // doesn't invalidate that cache). applyReadOverlay re-applies this set on every
    // fetch so the cleared dot stays cleared. Session-scoped, capped FIFO
    // (LinkedHashSet; see recordReadId in DenebClientMail.kt).
    internal val locallyReadMailIds = LinkedHashSet<String>()

    // Upcoming calendar events surfaced in the native calendar screen.
    internal val _denebCalendar = MutableStateFlow<List<CalendarEvent>>(emptyList())
    val denebCalendar: StateFlow<List<CalendarEvent>> = _denebCalendar

    // Pending calendar proposals (the calendar bell) — schedule-worthy items mail
    // analysis surfaced, awaiting accept/reject.
    internal val _denebCalProposals = MutableStateFlow<List<ai.deneb.deneb.generated.CalendarProposalOut>>(emptyList())
    val denebCalProposals: StateFlow<List<ai.deneb.deneb.generated.CalendarProposalOut>> = _denebCalProposals

    // Calendar month cache (range-key → when-fetched + events). The calendar
    // screen's own cache is composition-scoped, so every tab switch back to the
    // calendar re-hit Google for the visible month + both neighbors (~270ms each).
    // This client-level cache survives navigation, making a rapid re-open instant;
    // a short TTL bounds staleness and force=true (pull-to-refresh, after an edit)
    // bypasses it. Accessed only from the screen's Main-scoped coroutines.
    private val calRangeCache = mutableMapOf<String, Pair<TimeSource.Monotonic.ValueTimeMark, List<CalendarEvent>>>()

    /** Cached events for [key] if still within the TTL, else null. */
    internal fun cachedCalendarRange(key: String): List<CalendarEvent>? = calRangeCache[key]?.takeIf { it.first.elapsedNow() < CAL_RANGE_TTL }?.second

    /** Store a freshly-fetched range under [key], stamped now. */
    internal fun storeCalendarRange(key: String, events: List<CalendarEvent>) {
        calRangeCache[key] = TimeSource.Monotonic.markNow() to events
    }

    // Native-client handshake snapshot: gateway version, active model, and
    // feature flags exposed by miniapp.client.hello.
    private val _clientStatus = MutableStateFlow<ClientStatus?>(null)
    val clientStatus: StateFlow<ClientStatus?> = _clientStatus

    // Native work feed: proactive reports and native shares as actionable rows.
    // _denebWorkFeed holds the raw feed (all workspaces); the public flow is scoped
    // to the active workspace below.
    internal val _denebWorkFeed = MutableStateFlow<List<WorkFeedItem>>(emptyList())

    // False until the first work-feed fetch attempt finishes, so the 피드 home can
    // show a loading skeleton instead of flashing "오늘 받은 피드가 없습니다" before the
    // first list arrives (the raw flow seeds empty, so the list alone can't tell
    // "still loading" from "genuinely empty"). Reset on a credential switch.
    internal val _workFeedLoaded = MutableStateFlow(false)

    /** True once the work feed has been fetched at least once (success or a failed
     *  attempt) — lets the 피드 screen distinguish first-load from empty. */
    val workFeedLoaded: StateFlow<Boolean> = _workFeedLoaded

    // Reactive workspace mode (업무 true ↔ 챗봇 false), seeded from the persisted
    // recall setting and republished by [setWorkspace]. Drives the mode-scoped work
    // feed and gates proactive notifications. AppSettings is the source of truth on
    // disk; this mirror exists so flows can react to a switch.
    private val _workspaceWork = MutableStateFlow(appSettings.isRecallEnabled())

    /** Active workspace (업무 true ↔ 챗봇 false), republished by [setWorkspace]. The app
     *  shell reads this to adapt navigation per mode — 챗봇 hides 업무 데이터 sections
     *  (mail/calendar/search/categories/fleet). */
    val workspaceWork: StateFlow<Boolean> = _workspaceWork

    // 업무 and 챗봇 keep SEPARATE notification histories. The work feed (= the 업무
    // 알림 inbox) shown to the UI is scoped to the active workspace: 업무 shows its
    // proactive reports (client:main / non-chat items), 챗봇 shows only chat: items —
    // and since all proactive reports land in client:main, the 챗봇 feed is empty.
    // 업무 리포트 never bleeds into 챗봇. The raw feed still accumulates everything so
    // switching back to 업무 surfaces the reports that arrived meanwhile (조용히 쌓기).
    val denebWorkFeed: StateFlow<List<WorkFeedItem>> =
        combine(_denebWorkFeed, _workspaceWork) { items, work ->
            items.filter { isChatWorkspaceKey(it.sessionKey) != work }
        }.stateIn(scope, SharingStarted.Eagerly, emptyList())

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

    // Restored to the last-open session of the persisted workspace (업무 ↔ 챗봇),
    // so a restart reopens the space the user left, not always client:main.
    internal var sessionKey: String = resolveInitialSession()
        private set
    private val _currentConversationId = MutableStateFlow<String?>(sessionKey)
    override val currentConversationId: StateFlow<String?> = _currentConversationId

    internal val gatewayUrl: String
        get() = appSettings.settings.getString(KEY_URL, DEFAULT_URL).trimEnd('/')

    internal val clientToken: String
        get() = appSettings.settings.getString(KEY_TOKEN, "")

    // In-process guard for the window AFTER [callRpc] returns a (still-valid) result
    // but BEFORE the caller assigns it to a StateFlow: a state-mutating caller captures
    // credEpoch at its start and re-checks it before the assignment, so a gateway switch
    // landing in that window is honored. (The transport itself is fenced separately, by
    // credential-value comparison inside callRpc.) Bumped by [onCredentialsChanged].
    // @Volatile so a caller resuming on a background/Ktor thread sees the UI-thread bump.
    @Volatile
    internal var credEpoch: Int = 0
        private set

    /**
     * Apply a gateway URL/token change atomically (all synchronous, no suspension) so
     * there is no window where the fence state is inconsistent with the stored creds:
     *
     *   1. bump [credEpoch] FIRST — fences every already-in-flight RPC immediately,
     *      regardless of when the new creds are written;
     *   2. purge persisted caches BEFORE writing the new creds (crash-safe: a crash
     *      here leaves OLD creds + empty cache, never new creds + old cache);
     *   3. write the new creds in this same block, so the epoch is never bumped while
     *      settings still hold the old URL/token;
     *   4. wipe every gateway-backed StateFlow + reset native-sync, so nothing from
     *      account A is shown under account B until a fresh fetch succeeds.
     */
    fun onCredentialsChanged(newUrl: String, newToken: String) {
        credEpoch++
        appSettings.clearCachedContent()
        appSettings.settings.putString(KEY_URL, newUrl)
        appSettings.settings.putString(KEY_TOKEN, newToken)
        // Every gateway-backed StateFlow holds the OLD account's data; wipe them all so
        // nothing from account A is shown under account B until a fresh fetch succeeds.
        // (An in-flight fetch that started under A is dropped by callRpc's epoch+value
        // fence, so it can't repopulate these.)
        _chatHistory.value = emptyList()
        _savedConversations.value = emptyList()
        _denebMail.value = emptyList()
        _denebMailNativeStatus.value = null
        _denebMailNextToken.value = null
        denebMailActiveQuery = null
        locallyReadMailIds.clear()
        _denebWorkFeed.value = emptyList()
        _workFeedLoaded.value = false
        _hasUnreadWorkReport.value = false
        _hasUnreadHeartbeat.value = false
        _denebMemories.value = emptyList()
        _denebScheduledTasks.value = emptyList()
        _denebCalendar.value = emptyList()
        _denebCalProposals.value = emptyList()
        calRangeCache.clear()
        _denebModels.value = emptyList()
        _denebRoleModels.value = emptyMap()
        _denebModelAdvisories.value = emptyList()
        _denebMainHasVision.value = false
        _denebSkills.value = emptyList()
        _clientStatus.value = null
        // Reset the native-sync cursor + baseline so the new account replays its own
        // events from the start instead of inheriting account A's cursor (which could
        // skip B's events, or fire immediate notifications for catch-up events).
        nativeSyncCursor = 0L
        appSettings.settings.putLong(KEY_SYNC_CURSOR, 0L)
        nativeSyncBaselined = false
    }

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
        // Live progress: the gateway's tool/thinking SSE frames become transient
        // TOOL_EXECUTING rows so the waiting chip narrates what the agent is
        // doing ("메일 확인 중") instead of cycling generic spinner text.
        val progress = TurnProgress()
        val reply = try {
            sendStreaming(
                sendText,
                onTool = progress::onTool,
                onThinking = progress::onThinking,
            ) { delta ->
                progress.onDelta()
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
        } finally {
            // Progress rows are turn-scoped — never leak a zombie chip past the
            // turn, whatever way the stream ended (done, error, cancel).
            progress.clear()
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
        // Post-turn transparency: leave a compact trail of what the agent did
        // ("메일 확인 ×2 · 웹 검색") under the finished answer.
        progress.footprint()?.let { fp ->
            _chatHistory.update { list ->
                list.map { if (it.id == assistantId) it.copy(toolFootprint = fp) else it }
            }
        }
    }

    private fun formatCallback(submission: UiSubmission): String = buildString {
        append("[deneb-ui] event=").append(submission.pressedEvent)
        if (submission.values.isNotEmpty()) {
            append(" values={")
            append(submission.values.entries.joinToString(", ") { "${it.key}=${it.value}" })
            append("}")
        }
    }

    /**
     * Turn-scoped live progress for [ask]: gateway `tool`/`thinking` SSE frames
     * become transient [History.Role.TOOL_EXECUTING] rows (status-only, Korean
     * labels via [ToolStatusLabels]) that the chat screen's waiting chip picks
     * up through its existing executing-tools derivation — the same mechanism
     * the local-provider pipeline uses.
     *
     * Coverage goal: never regress the chip to the generic spinner mid-turn.
     * Thinking frames narrate the live reasoning tail ("깊이 생각 중: …"), and
     * when the last running tool completes the row is repurposed as a
     * continuity status ("결과 검토 중…") that bridges the event-silent prefill
     * stretch until the next thinking/tool/delta event.
     *
     * Threading: all map/flag state is touched only from the SSE read coroutine
     * (callbacks run inline in [sendStreaming]); the delayed removals/swaps
     * launched on [scope] only perform id-keyed history edits, so no
     * synchronization is needed. [clear] runs in ask()'s finally, so a
     * mid-stream error or cancel can never leak a zombie chip row.
     */
    private inner class TurnProgress {
        private val thinkingId = "progress-thinking-${Uuid.random()}"
        private var thinkingVisible = false

        // toolUseId (or tool name when the gateway omits the id) → row id/start.
        private val rowIds = mutableMapOf<String, String>()
        private val startMarks = mutableMapOf<String, TimeSource.Monotonic.ValueTimeMark>()
        private val allRowIds = mutableSetOf<String>()

        // Row currently repurposed as the between-steps continuity status
        // ("결과 검토 중…"); null when no continuity chip is showing.
        private var continuityRowId: String? = null

        // Completed tools in execution order (tool name + error flag) — the
        // source of the post-turn footprint line under the answer.
        private val trail = mutableListOf<Pair<String, Boolean>>()

        /**
         * Reasoning liveness pulse → show "깊이 생각 중…" until text or a tool
         * arrives. [preview] is a chip-sized tail of the live reasoning text
         * (server-throttled to ~1 frame / 2s); when present the row narrates
         * the actual thought — "깊이 생각 중: …발신인 이력을 대조" — and each
         * pulse refreshes it.
         */
        fun onThinking(preview: String) {
            hideContinuity()
            val label = ToolStatusLabels.THINKING +
                if (preview.isNotEmpty()) ": $preview" else ""
            if (!thinkingVisible) {
                thinkingVisible = true
                allRowIds += thinkingId
                _chatHistory.update { list ->
                    list + History(
                        id = thinkingId,
                        role = History.Role.TOOL_EXECUTING,
                        content = "thinking",
                        toolName = label,
                        isStatusMessage = true,
                    )
                }
            } else if (preview.isNotEmpty()) {
                _chatHistory.update { list ->
                    list.map { if (it.id == thinkingId) it.copy(toolName = label) else it }
                }
            }
        }

        /** Visible answer text is flowing — drop the status rows (O(1) when hidden). */
        fun onDelta() {
            hideThinking()
            hideContinuity()
        }

        fun onTool(ev: ToolEvent) {
            val key = ev.toolUseId.ifEmpty { ev.tool }
            when (ev.state) {
                "started" -> {
                    hideThinking()
                    hideContinuity()
                    val rowId = "progress-tool-${Uuid.random()}"
                    rowIds[key] = rowId
                    startMarks[key] = TimeSource.Monotonic.markNow()
                    allRowIds += rowId
                    // "메일 확인 중: 아르고에너지" — the server-extracted hint
                    // names the target, not just the tool.
                    val label = ToolStatusLabels.label(ev.tool) +
                        if (ev.detail.isNotEmpty()) ": ${ev.detail}" else ""
                    _chatHistory.update { list ->
                        list + History(
                            id = rowId,
                            role = History.Role.TOOL_EXECUTING,
                            content = ev.tool,
                            toolName = label,
                            isStatusMessage = true,
                        )
                    }
                }

                "completed" -> {
                    trail += ev.tool to ev.isError
                    val rowId = rowIds.remove(key) ?: return
                    if (ev.isError) {
                        // Swap the row to its failure form ("메일 확인 실패")
                        // and hold it readable — the agent usually keeps going,
                        // so this explains why the turn is taking longer.
                        val failure = ToolStatusLabels.failureLabel(ev.tool)
                        _chatHistory.update { list ->
                            list.map { if (it.id == rowId) it.copy(toolName = failure) else it }
                        }
                        scope.launch {
                            delay(FAILURE_DISPLAY_MS.milliseconds)
                            removeRow(rowId)
                        }
                        startMarks.remove(key)
                        return
                    }
                    val elapsed = startMarks.remove(key)?.elapsedNow() ?: 0.milliseconds
                    val remaining = MIN_PROGRESS_DISPLAY_MS.milliseconds - elapsed
                    if (rowIds.isEmpty()) {
                        // Last running tool finished — the model is back in an
                        // LLM step reading the results, which on a cache-missed
                        // prefill can stay event-silent for tens of seconds.
                        // Repurpose the row as a continuity status instead of
                        // dropping the chip back to the generic spinner; the
                        // next thinking/tool/delta event (or clear) removes it.
                        continuityRowId = rowId
                        val swap = {
                            _chatHistory.update { list ->
                                list.map {
                                    if (it.id == rowId) {
                                        it.copy(content = "continuity", toolName = ToolStatusLabels.REVIEWING)
                                    } else {
                                        it
                                    }
                                }
                            }
                        }
                        if (remaining.isPositive()) {
                            // Keep the finished tool's label readable first. The
                            // delayed swap is an idempotent id-keyed map, so
                            // racing hideContinuity()/clear() (row already gone)
                            // is harmless.
                            scope.launch {
                                delay(remaining)
                                swap()
                            }
                        } else {
                            swap()
                        }
                        return
                    }
                    if (remaining.isPositive()) {
                        // Hold fast tools on screen long enough to read; the
                        // removal is an idempotent id filter, so racing clear()
                        // or a conversation switch is safe.
                        scope.launch {
                            delay(remaining)
                            removeRow(rowId)
                        }
                    } else {
                        removeRow(rowId)
                    }
                }
            }
        }

        /**
         * One-line trail of what this turn did — "메일 확인 ×2 · 웹 검색 ⚠" —
         * attached under the finished answer. Null when no tool completed.
         * Live-turn only by design: the gateway transcript does not carry it,
         * so reloading a conversation drops the line.
         */
        fun footprint(): String? {
            if (trail.isEmpty()) return null
            val counts = LinkedHashMap<String, IntArray>() // tool → [count, errored(0/1)]
            for ((tool, isError) in trail) {
                val agg = counts.getOrPut(tool) { intArrayOf(0, 0) }
                agg[0]++
                if (isError) agg[1] = 1
            }
            val parts = counts.entries.take(FOOTPRINT_MAX_TOOLS).map { (tool, agg) ->
                buildString {
                    append(ToolStatusLabels.trailLabel(tool))
                    if (agg[0] > 1) append(" ×${agg[0]}")
                    if (agg[1] == 1) append(" ⚠")
                }
            }
            val more = counts.size - FOOTPRINT_MAX_TOOLS
            return parts.joinToString(" · ") + if (more > 0) " 외 $more" else ""
        }

        /** Remove every row this turn added (idempotent; runs in ask()'s finally). */
        fun clear() {
            if (allRowIds.isEmpty()) return
            thinkingVisible = false
            continuityRowId = null
            val ids = allRowIds.toSet()
            _chatHistory.update { list -> list.filter { it.id !in ids } }
        }

        private fun hideThinking() {
            if (!thinkingVisible) return
            thinkingVisible = false
            removeRow(thinkingId)
        }

        private fun hideContinuity() {
            val id = continuityRowId ?: return
            continuityRowId = null
            removeRow(id)
        }

        private fun removeRow(id: String) {
            _chatHistory.update { list -> list.filter { it.id != id } }
        }
    }

    override fun clearHistory() {
        _chatHistory.value = emptyList()
    }

    // Drop the last user message and everything after it (its assistant reply).
    // Operates on the gateway client's own [_chatHistory] (the visible one). Used
    // by regenerate() before it re-asks.
    override fun popLastExchange() {
        _chatHistory.update { history ->
            val lastUserIndex = history.indexOfLast { it.role == History.Role.USER }
            if (lastUserIndex >= 0) history.take(lastUserIndex) else history
        }
    }

    override fun startNewChat() {
        _chatHistory.value = emptyList()
        // A fresh independent conversation in the CURRENT workspace: 업무 branches
        // off its home (client:main:<uuid>), 챗봇 mints a flat chat:<uuid> with no
        // home — so the two session lists never mix.
        switchSession(newSessionKey(appSettings.isRecallEnabled()))
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
    private suspend fun loadTranscriptGuarded(key: String, replacing: Boolean = false) {
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
     * Open the client:main home conversation where proactive reports are mirrored
     * — the deep-link target when the user taps a proactive-report push. Guarded so
     * a concurrent cold-start share can't be clobbered (see historyGate).
     */
    fun openWorkTopic() {
        // Proactive reports live in the 업무 workspace, so opening one from a push
        // also switches the active workspace to 업무 (recall on) — otherwise the
        // user would land on client:main while the pill still said 챗봇.
        setWorkspace(true)
        switchSession("client:main")
        syncNativeStateAsync()
        // Deep-link switch to the work home: replace whatever conversation was open
        // (cold-start callers are already empty-guarded, so this is a no-op there).
        scope.launch { loadTranscriptGuarded("client:main", replacing = true) }
        loadConversations()
    }

    /**
     * Mint a fresh independent session key for a workspace. 업무 branches off its
     * home (client:main:<uuid>); 챗봇 has no home, so each chat is a flat,
     * independent chat:<uuid>.
     */
    private fun newSessionKey(work: Boolean): String = if (work) "client:main:${Uuid.random()}" else "chat:${Uuid.random()}"

    /**
     * The session to OPEN when entering a workspace: the last one used, or — when
     * 챗봇 has none yet (blank default) — a freshly minted chat:<uuid>. 업무 always
     * defaults to its client:main home, so it never mints here.
     */
    private fun openSessionFor(work: Boolean): String = appSettings.lastSession(work).ifBlank { newSessionKey(work) }

    /**
     * Initial active session at construction: the persisted last session, or — for
     * a 챗봇 first run with no home — a freshly minted chat:<uuid> persisted right
     * away so the next cold start restores it. (switchSession persists thereafter.)
     */
    private fun resolveInitialSession(): String {
        val work = appSettings.isRecallEnabled()
        val stored = appSettings.lastSession(work)
        if (stored.isNotBlank()) return stored
        val minted = newSessionKey(work)
        appSettings.setLastSession(work, minted)
        return minted
    }

    /**
     * Single writer for the active workspace mode: persist it (AppSettings is the
     * on-disk source of truth) and republish to [_workspaceWork] so the mode-scoped
     * work feed and the proactive-notification gates react. Entering 챗봇 also clears
     * the 업무 리포트 unread badge — that banner belongs to the 업무 workspace.
     */
    private fun setWorkspace(work: Boolean) {
        appSettings.setRecallEnabled(work)
        _workspaceWork.value = work
        if (!work) _hasUnreadWorkReport.value = false
    }

    /**
     * Switch the active workspace (업무 ↔ 챗봇). Each keeps its OWN session list
     * and recall behavior, so this flips recall, restores that workspace's last
     * session, and refreshes the (now mode-filtered) drawer. The persona is
     * unchanged — only which session space + whether recall fires.
     */
    fun switchWorkspace(toWork: Boolean) {
        if (appSettings.isRecallEnabled() == toWork) return
        setWorkspace(toWork)
        val target = openSessionFor(toWork)
        _chatHistory.value = emptyList()
        switchSession(target)
        syncNativeStateAsync()
        scope.launch { loadTranscriptGuarded(target) }
        loadConversations()
    }

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
    suspend fun openWorkTopicAtItem(item: WorkFeedItem): String? {
        setWorkspace(true) // work-feed cards belong to the 업무 workspace
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

    /**
     * Cold-start home = the client:main 업무 topic, where proactive reports
     * (morning-letter, mail-analysis) are mirrored. Open it so those reports are
     * visible by default instead of an empty chat.
     *
     * Guarded so a settings refresh (refreshSettings re-calls this) or a
     * cold-start share can't yank the user out of what they're viewing: only
     * open the home when nothing is loaded yet and we are still on the default
     * home session.
     */
    override fun restoreCurrentConversation() {
        if (_chatHistory.value.isNotEmpty()) return
        when {
            // 업무 home pulls in the mirrored proactive reports via openWorkTopic.
            sessionKey == "client:main" -> openWorkTopic()

            // 챗봇 sessions are plain general chats (flat chat:<uuid>, no home) — just
            // load the last one's transcript so a cold start restores it.
            isChatWorkspaceKey(sessionKey) -> {
                syncNativeStateAsync()
                scope.launch { loadTranscriptGuarded(sessionKey) }
            }
        }
    }

    /**
     * A proactive report just landed in client:main while the app is foregrounded
     * (so the scheduler raised no notification). If the user is already on the
     * home transcript, reload it so the report appears live — the SSE push frame
     * carries only a one-line preview, not the body. Otherwise raise the unread
     * badge so the in-app banner points them at the work topic.
     */
    override fun onProactiveReportForeground() {
        syncNativeStateAsync()
        // 챗봇 모드에서는 업무 리포트 배지/배너를 올리지 않는다 (도착은 피드에 조용히 쌓임).
        if (!_workspaceWork.value) return
        if (sessionKey == "client:main") {
            scope.launch { loadTranscriptGuarded("client:main") }
        } else {
            // Not on the work home — raise the in-app unread badge so the banner
            // points the user at the 업무 topic.
            _hasUnreadWorkReport.value = true
        }
    }

    fun refreshWorkFeedAsync() {
        scope.launch { refreshWorkFeed() }
    }

    fun refreshWorkFeedRangeAsync(sinceMs: Long, beforeMs: Long) {
        scope.launch { refreshWorkFeed(sinceMs = sinceMs, beforeMs = beforeMs, merge = true) }
    }

    fun syncNativeStateAsync() {
        scope.launch { syncNativeState() }
    }

    suspend fun syncNativeState(): Boolean {
        val epoch = credEpoch
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
                // Credentials switched after this page returned: stop before applying
                // account A's events (work-feed/transcript mutations, notifications) or
                // advancing the cursor under account B.
                if (epoch != credEpoch) return false
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
            // Credentials switched while this sync held the gate: onCredentialsChanged
            // reset the cursor/baseline OUTSIDE the gate, so a cursor we advanced above
            // could otherwise survive and make account B inherit account A's cursor.
            // Re-assert the reset here (still under the gate) so B replays from the start.
            if (epoch != credEpoch) {
                nativeSyncCursor = 0L
                appSettings.settings.putLong(KEY_SYNC_CURSOR, 0L)
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
        if (eventCount == 0 && _denebWorkFeed.value.isEmpty()) {
            refreshWorkFeed()
        }
        return true
    }

    suspend fun refreshWorkFeed(sinceMs: Long = 0L, beforeMs: Long = 0L, merge: Boolean = false): Boolean {
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
        val incoming = payload.items.filter { it.id.isNotBlank() }
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
        return true
    }

    /** In-app browser in-place translation (en/ru → ko): ships the page's text
     *  segments to miniapp.web.translate and returns a SAME-length, SAME-order
     *  list of translations. Null on transport/auth failure or when the
     *  translation role is unwired; the JS bridge then keeps the originals. */
    @Serializable
    private data class TranslatePayload(val translated: List<String> = emptyList())

    internal suspend fun translateSegments(segments: List<String>, targetLang: String = "ko"): List<String>? {
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
    internal suspend fun observeBehavior(days: Int): ObserveBehavior? = callRpc("miniapp.observe.behavior", buildJsonObject { put("days", days) })

    internal suspend fun observeLogs(level: String, limit: Int, days: Int = 0): ObserveLogsPayload? = callRpc(
        "miniapp.observe.logs",
        buildJsonObject {
            put("level", level)
            put("limit", limit)
            if (days > 0) put("days", days)
        },
    )

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
        loadTranscriptGuarded(target, replacing = true)
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
    suspend fun sendWorkFeedFeedback(itemId: String, feedback: String): String? {
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
    suspend fun rewriteWorkFeedCard(itemId: String): String? {
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

    private fun decodeWorkFeedActionRun(payload: JsonObject?): NativeSyncActionPayload? = runCatching {
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
            sortWorkFeedItems(next)
        }
    }

    private fun sortWorkFeedItems(items: List<WorkFeedItem>): List<WorkFeedItem> = items.sortedWith(
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
    private fun maybeEmitProactiveNotification(item: WorkFeedItem) {
        if (!nativeSyncBaselined) return
        if (item.id.isBlank() || item.status != "unread") return
        // 챗봇 모드에서는 업무 리포트가 도달하지 않는다: only notify when the item's
        // workspace matches the active one. A 업무 item (client:main proactive report)
        // arriving while in 챗봇 raises no tray notification — it sits silently in the
        // raw feed and surfaces when the user switches back to 업무 (조용히 쌓기).
        if (isChatWorkspaceKey(item.sessionKey) == _workspaceWork.value) return
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
            val epoch = credEpoch
            // Keep the current list when the fetch fails (null) so a transient
            // sessions.recent RPC error doesn't flap the drawer between the full
            // list and just the 업무 home row.
            val fresh = fetchRecentSessions() ?: return@launch
            // Credentials switched mid-fetch — don't repopulate the drawer with the
            // old account's private session titles under the new gateway.
            if (epoch != credEpoch) return@launch
            _savedConversations.value = fresh
        }
    }

    override fun loadConversation(id: String) {
        switchSession(id)
        // Explicit switch from the drawer: replace the previously-visible conversation
        // so it can't linger under the new sessionKey if the fetch fails.
        scope.launch { loadTranscriptGuarded(id, replacing = true) }
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
        // Drop the local transcript cache too, so the deleted conversation can't be
        // instantly re-rendered from cache on a later reopen. (A still-live session is
        // refused server-side and reappears on the next sessions.recent fetch; its
        // cache will be rebuilt then — eviction here is harmless in that case.)
        removeCachedTranscript(id)
        _savedConversations.update { list -> list.filterNot { it.id == id } }
    }

    // --- Memory screen → Deneb wiki (read-only browser) ---------------------
    // Wiki pages ([denebMemories]) and Deneb crons ([denebScheduledTasks]) are
    // surfaced to their screens through the concrete StateFlows + refresh
    // extensions in DenebClientMemory.kt / DenebClientAdmin.kt (refreshMemories,
    // refreshScheduledTasks, removeCron) — not the DataRepository interface, which
    // no longer carries on-device memory/scheduling members.

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
                        skipRecall = !appSettings.isRecallEnabled(),
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
     * gateway also frames live progress — [onTool] fires on tool lifecycle
     * transitions (state "started"/"completed") and [onThinking] on throttled
     * reasoning liveness pulses (carrying a chip-sized preview of the live
     * reasoning text, "" on older gateways) — which [ask] surfaces in the
     * waiting chip. The
     * terminal `done` frame carries the canonical text + which model answered,
     * returned as the [GatewayReply]. Throws on transport failure or a server
     * `error` frame so [ask] can fall back to the blocking RPC.
     *
     * SSE is parsed by hand off the response channel (no Ktor SSE plugin): lines
     * accumulate per frame and dispatch on the blank-line separator. Comment
     * lines (": keepalive") and unknown events are ignored, so an older gateway
     * without tool/thinking frames degrades gracefully. The request timeout is
     * disabled because an agent turn (tool calls included) can outlast the
     * default window.
     */
    private suspend fun sendStreaming(
        message: String,
        onTool: (ToolEvent) -> Unit = {},
        onThinking: (String) -> Unit = {},
        onDelta: (String) -> Unit,
    ): GatewayReply {
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
            setBody(SendParams(message = message, sessionKey = sessionKey, skipRecall = !appSettings.isRecallEnabled()))
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
                    line.startsWith(":") -> Unit

                    // comment / keepalive
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

                            "tool" -> {
                                runCatching {
                                    jsonCodec.decodeFromString(ToolEvent.serializer(), data.toString())
                                }.getOrNull()?.let {
                                    if (it.tool.isNotEmpty()) onTool(it)
                                }
                            }

                            "thinking" -> {
                                // preview: chip-sized tail of the live reasoning
                                // text; absent on bare liveness pulses (and on
                                // older gateways, which send an empty object).
                                val preview = runCatching {
                                    jsonCodec.decodeFromString(ThinkingEvent.serializer(), data.toString()).preview
                                }.getOrNull() ?: ""
                                onThinking(preview)
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
                            line.startsWith(":") -> Unit

                            // keepalive comment
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
        // Remember this as the active session of ITS workspace (by key namespace),
        // so switching the pill back restores where each space was left.
        appSettings.setLastSession(work = !key.startsWith("chat:"), key)
    }

    // Returns null on an RPC failure (so callers can keep a cache render instead of
    // flashing to empty), or the messages — possibly an authoritative empty list —
    // on success. The null-vs-[] distinction is what lets loadTranscriptGuarded
    // evict a stale cache only when the server says the session is really empty.
    private suspend fun fetchTranscript(sessionKey: String): List<History>? {
        val payload = callRpc<TranscriptPayload>(
            "miniapp.sessions.transcript",
            buildJsonObject {
                put("sessionKey", sessionKey)
                put("limit", 200)
            },
        ) ?: return null
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
                History(role = role, content = m.content, attachments = attachments, timestampMs = m.timestampMs)
            }
        }
    }

    /**
     * Generic POST to the miniapp RPC bridge. Returns the typed payload, or null
     * on any failure (missing token, transport error, non-ok response) so callers
     * degrade to empty rather than crash. Use this for non-critical reads; the
     * chat [send] keeps its own throwing path so the UI can surface errors.
     * Internal (not private) so the per-domain extension files can reach it.
     *
     * Credential fence (two complementary checks; the result is dropped to null if
     * either trips, so an old-account response can never be assigned under new
     * credentials — callers already treat null as "no data"):
     *   - credEpoch: bumped FIRST and atomically in [onCredentialsChanged], so every
     *     already-in-flight request is fenced the instant a switch begins, even before
     *     the new URL/token are written.
     *   - URL+token value: the exact values the request was SENT with vs. current.
     *     Ordering-immune for the post-write case (a counter alone could be fooled).
     * This is the single chokepoint protecting EVERY read (mail, transcript, sessions,
     * work-feed, calendar, memories, models, skills, …).
     */
    internal suspend inline fun <reified T> callRpc(method: String, params: JsonObject): T? {
        val url = gatewayUrl
        val token = clientToken
        val epoch = credEpoch
        if (token.isEmpty()) return null
        val payload = runCatching {
            http.post("$url/api/v1/miniapp/rpc") {
                header(CLIENT_TOKEN_HEADER, token)
                contentType(ContentType.Application.Json)
                setBody(RpcReq(id = Uuid.random().toString(), method = method, params = params))
            }.body<RpcEnv<T>>().payload
        }.getOrNull()
        return if (epoch == credEpoch && url == gatewayUrl && token == clientToken) payload else null
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

    /**
     * Registers this device's FCM registration token so the gateway can deliver
     * proactive reports when no live SSE connection is held (app fully closed /
     * Doze). Best-effort and idempotent — the gateway dedups by token — so it is
     * cheap to call on every foreground. Returns true on success. Android-only
     * caller, but the RPC itself is platform-agnostic so this lives in commonMain.
     */
    suspend fun registerPushToken(token: String, platform: String): Boolean {
        if (token.isBlank()) return false
        return rpcWrite(
            "miniapp.push.register",
            buildJsonObject {
                put("token", token)
                put("platform", platform)
            },
        ) == null
    }

    /** Removes a device token (e.g. on sign-out / token invalidation). */
    suspend fun unregisterPushToken(token: String): Boolean {
        if (token.isBlank()) return false
        return rpcWrite(
            "miniapp.push.unregister",
            buildJsonObject { put("token", token) },
        ) == null
    }

    @Serializable
    private data class RpcRequest(val id: String, val method: String, val params: SendParams)

    @Serializable
    private data class SendParams(
        val message: String,
        val sessionKey: String? = null,
        // "focused chat / memory off" toggle: true skips the gateway's recall
        // (and retain) for this turn. Default false (recall on) is omitted by the
        // encoder, so an older gateway simply ignores the absent field.
        val skipRecall: Boolean = false,
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

    // detail: short hint extracted from the tool input server-side (query,
    // command, file name); isError marks a completed tool that returned an error.
    @Serializable
    private data class ToolEvent(
        val state: String = "",
        val tool: String = "",
        val toolUseId: String = "",
        val detail: String = "",
        val isError: Boolean = false,
    )

    // preview: chip-sized tail of the live reasoning text the gateway condenses
    // from the model's thinking stream; empty on bare liveness pulses.
    @Serializable
    private data class ThinkingEvent(val preview: String = "")

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
    // --- DataRepository: non-chat surface -----------------------------------
    // The small set of non-chat DataRepository members the UI still reaches
    // through the interface. Re-homed here when the on-device RemoteDataRepository
    // (and its `by base` delegation) was removed; backed by appSettings + the SMS
    // draft store + self-contained notification-pulse flows.

    // The gateway is the single backend, so there is no client-side provider
    // fallback ladder — this stays null. Kept only to satisfy the chat fallback
    // banner, which renders nothing when null.
    override val fallbackStatus: StateFlow<FallbackStatus?> = MutableStateFlow(null)

    override fun isRecallEnabled(): Boolean = appSettings.isRecallEnabled()

    // Gateway-side document extraction accepts the same set the on-device OpenAI
    // service used to advertise (images + text + pdf). The file picker filters by
    // this list before an attachment is sent to ask().
    override fun supportedFileExtensions(): List<String> = ai.deneb.data.supportedFileExtensions + "pdf"

    override fun truncateFrom(messageId: String) {
        // Operate on the gateway client's own history (the visible one). The old
        // delegated impl mutated RemoteDataRepository's separate flow and had no
        // visible effect here — the same gotcha popLastExchange documents.
        _chatHistory.update { history ->
            val index = history.indexOfFirst { it.id == messageId }
            if (index >= 0) history.take(index) else history
        }
    }

    // SMS drafts: the gateway proposes a draft, the user approves it via the in-app
    // banner, and the phone sends it. Explicitly user-triggered (never AI-triggered)
    // — the banner is the gate.
    override val smsDrafts: StateFlow<List<SmsDraft>> = smsDraftStore.drafts

    override suspend fun sendSmsDraft(draftId: String): Boolean {
        val draft = smsDraftStore.getDraft(draftId) ?: return false
        if (draft.status != SmsDraftStatus.PENDING) return false
        smsDraftStore.updateStatus(draftId, SmsDraftStatus.SENDING)
        return when (val result = smsSender.send(draft.address, draft.body)) {
            is SmsSendResult.Success -> {
                smsDraftStore.updateStatus(draftId, SmsDraftStatus.SENT)
                true
            }

            is SmsSendResult.Failure -> {
                smsDraftStore.updateStatus(draftId, SmsDraftStatus.FAILED, result.message)
                false
            }
        }
    }

    override suspend fun discardSmsDraft(draftId: String) {
        smsDraftStore.removeDraft(draftId)
    }

    // Heartbeat / work-report notification pulses. Set by MainActivity (Android
    // push tap → requestOpen*) and by onProactiveReportForeground; collected by
    // ChatViewModel. Self-contained flows with no backend. Note: hasUnreadHeartbeat
    // is never raised in gateway mode (the old on-device heartbeat that set it is
    // gone); the flow stays so the badge wiring keeps compiling.
    private val _hasUnreadHeartbeat = MutableStateFlow(false)
    override val hasUnreadHeartbeat: StateFlow<Boolean> = _hasUnreadHeartbeat
    override fun clearUnreadHeartbeat() {
        _hasUnreadHeartbeat.value = false
    }

    private val _openHeartbeatRequested = MutableStateFlow(false)
    override val openHeartbeatRequested: StateFlow<Boolean> = _openHeartbeatRequested
    override fun requestOpenHeartbeat() {
        _openHeartbeatRequested.value = true
    }
    override fun consumeOpenHeartbeatRequest() {
        _openHeartbeatRequested.value = false
    }

    private val _openWorkTopicRequested = MutableStateFlow(false)
    override val openWorkTopicRequested: StateFlow<Boolean> = _openWorkTopicRequested
    override fun requestOpenWorkTopic() {
        _openWorkTopicRequested.value = true
    }
    override fun consumeOpenWorkTopicRequest() {
        _openWorkTopicRequested.value = false
    }

    private val _hasUnreadWorkReport = MutableStateFlow(false)
    override val hasUnreadWorkReport: StateFlow<Boolean> = _hasUnreadWorkReport
    override fun clearUnreadWorkReport() {
        _hasUnreadWorkReport.value = false
    }

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

        // Minimum on-screen time for a tool progress row in the waiting chip,
        // so fast tools register as a readable label instead of a flicker.
        const val MIN_PROGRESS_DISPLAY_MS = 1_500L

        // How long a failed tool's "~ 실패" label stays in the chip before the
        // turn moves on — long enough to read, short enough not to alarm.
        const val FAILURE_DISPLAY_MS = 1_800L

        // Cap on distinct tools named in the post-turn footprint line.
        const val FOOTPRINT_MAX_TOOLS = 5

        // Max idle between bytes on the chat SSE stream. The server emits a
        // keepalive comment every 15s, so this only trips on a real stall.
        const val STREAM_SOCKET_TIMEOUT_MS = 120_000L

        // How long a fetched calendar month is served from the client cache before
        // a re-open refetches. Short enough that an event added elsewhere surfaces
        // soon (and pull-to-refresh / edits force immediately), long enough that
        // rapid tab-switching back to the calendar is instant.
        val CAL_RANGE_TTL = 120.seconds
    }
}
