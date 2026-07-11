package ai.deneb.deneb

import ai.deneb.data.AppSettings
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
import io.ktor.client.request.preparePost
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsChannel
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import io.ktor.utils.io.ByteReadChannel
import io.ktor.utils.io.readByte
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.concurrent.Volatile
import kotlin.time.Duration.Companion.hours
import kotlin.time.Duration.Companion.minutes
import kotlin.time.Duration.Companion.seconds
import kotlin.time.TimeSource
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

// SSE lines, UTF-8-safe. Ktor 3.x readUTF8Line decoded multibyte characters per internal
// network segment, so a 3-byte Korean character split across the buffer boundary surfaced
// as `�` on the native client (both the streamed chat reply and proactive-push text; the
// browser/CIO paths were unaffected). Accumulating raw bytes until the ASCII newline —
// which never falls inside a multibyte UTF-8 sequence — then decoding the whole line fixes
// it. readByte() is per-byte, which is fine for SSE's low frame volume. An exception thrown
// by [onLine] (e.g. the gateway "error" event) propagates; only readByte's EOF/close is
// swallowed so a normal stream end isn't an error.
internal suspend fun readSseLines(channel: ByteReadChannel, onLine: (String) -> Unit) {
    val line = ArrayList<Byte>(256)
    fun emit() {
        val text = line.toByteArray().decodeToString()
        line.clear()
        onLine(if (text.endsWith('\r')) text.dropLast(1) else text)
    }
    while (!channel.isClosedForRead) {
        val b = try {
            channel.readByte()
        } catch (c: CancellationException) {
            throw c
        } catch (_: Exception) {
            break
        }
        if (b.toInt() == 10) emit() else line.add(b)
    }
    if (line.isNotEmpty()) emit()
}

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
    // internal (not private) so the facade's extension files (sessions, admin,
    // patch notes) can read persisted settings.
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
    internal val historyGate = Mutex()

    // True while ask() drives a turn. Background transcript reconciles (events
    // stream reconnect) must not touch the view then — the live stream, or its
    // stream-failure recovery, owns it. Volatile: read from the daemon's events
    // coroutine on another thread; the worst race is one skipped reconcile.
    @Volatile
    internal var askActive = false
    internal var historyEpoch = 0L
    internal val nativeSyncGate = Mutex()
    internal var nativeSyncCursor = appSettings.settings.getLong(KEY_SYNC_CURSOR, 0L)

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

    // One surface, one dataset: the feed exposed to the UI is the full work feed
    // (persona/workspace splits are UI-forbidden — see CLAUDE.md).
    val denebWorkFeed: StateFlow<List<WorkFeedItem>> = _denebWorkFeed.asStateFlow()

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
    internal val _proactiveNotifications =
        MutableSharedFlow<ProactiveNotification>(extraBufferCapacity = 32)
    val proactiveNotifications: SharedFlow<ProactiveNotification> =
        _proactiveNotifications.asSharedFlow()

    // The first post-launch sync is a catch-up over everything accumulated while
    // the app was closed: surface those into the feed but suppress notifications so
    // opening the app doesn't fire a barrage. Only items pulled after this is set
    // raise a notification. Read/written only under nativeSyncGate, so the gate's
    // happens-before covers visibility without @Volatile.
    internal var nativeSyncBaselined = false

    // Throttle for background home-cache warming. Every SSE frame / FCM wake / foreground
    // resume funnels through syncNativeState(); without this gate each one would re-fetch
    // calendar + mail. Written/read only on the sync coroutine (serialized), so no lock.
    internal var lastHomeWarm: TimeSource.Monotonic.ValueTimeMark? = null

    // Throttle for the app-usage digest forward (sensing, "read broad, surface narrow").
    // syncNativeState fires often, but a usage digest only goes to the gateway every
    // USAGE_FORWARD_INTERVAL — per-sync forwarding would churn low-value state.
    // Set even when the read returns null (no permission / no signal) so we don't probe
    // UsageStats on every sync. Serialized on the sync coroutine, so no lock.
    internal var lastUsageForward: TimeSource.Monotonic.ValueTimeMark? = null

    // Throttle for the on-demand location forward (same "read broad, surface narrow"
    // sensing model). Pushed every LOCATION_FORWARD_INTERVAL so phone_read has a recent
    // fix without per-sync battery cost. Set even when the read returns null (no
    // permission / no fix). Serialized on the sync coroutine, so no lock.
    internal var lastLocationForward: TimeSource.Monotonic.ValueTimeMark? = null

    // Restored to the persisted last-open session, so a restart reopens the
    // conversation the user left, not always client:main.
    internal var sessionKey: String = appSettings.lastSession()
        internal set
    internal val _currentConversationId = MutableStateFlow<String?>(sessionKey)
    override val currentConversationId: StateFlow<String?> = _currentConversationId

    internal val gatewayUrl: String
        get() = appSettings.settings.getString(KEY_URL, DEFAULT_URL).trimEnd('/')

    internal val clientToken: String
        get() = appSettings.settings.getString(KEY_TOKEN, "")

    init {
        // Cache-then-network: seed the feed and calendar from the last-known briefing
        // so a cold start renders instantly and, when the gateway is unreachable (the
        // offline-first launcher shell), the home/calendar aren't empty. The network
        // refresh (refreshWorkFeed/refreshCalendar) overwrites with the authoritative
        // lists when it succeeds.
        loadCachedWorkFeed()?.let { _denebWorkFeed.value = it }
        loadCachedCalendar()?.let { _denebCalendar.value = it }
    }

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

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?): Boolean {
        val displayText = question?.trim().orEmpty()
        // A deneb-ui button press arrives as a UiSubmission. Show the friendly
        // question in the chat, but send the agent a structured callback naming
        // the event (per the deneb-ui prompt contract) plus the collected inputs.
        val sendText = if (uiSubmission != null) formatCallback(uiSubmission) else displayText
        if (sendText.isEmpty()) return true // no-op, not a failure

        // Append the user message + a placeholder assistant bubble (grown as
        // deltas stream in; replacement is keyed by id so concurrent history
        // edits stay safe). Bump the epoch and append under historyGate so a
        // background transcript load in flight — the cold-start topic
        // auto-select — can't overwrite them (see historyGate).
        val assistantId = Uuid.random().toString()
        // Pinned for the stream-failure recovery below: the transcript to
        // reconcile against is the one this turn was SENT to, even if the user
        // switches conversations while recovery is polling.
        val sessionKeyAtSend = sessionKey
        askActive = true
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

        // Token pacer. The gateway emits a delta per token (~65/s, smooth at the
        // source — measured directly), but the network coalesces those tiny frames
        // into ~170ms bursts over a relay/Wi-Fi link, so painting arrivals as-is
        // looks like "one char → stall → burst". Instead we REVEAL the accumulated
        // text at a steady cadence: a ticker advances a `revealed` cursor toward
        // accumulated.length, draining a proportional slice each tick so a burst
        // crawls out smoothly and a small constant buffer (~one tick of lag) absorbs
        // the next one. The finalize block below always writes the full canonical
        // text, so the trailing buffer is never lost. Cost stays bounded — the VM
        // samples chatHistory (64ms) and BotMessage re-parses (96ms) downstream, so
        // these steady reveals coalesce there.
        //
        // Thread-safety: ask() runs on a background (multi-thread) dispatcher, so a
        // pacer launched there could race onDelta on `accumulated`. We pin just this
        // streaming section to Dispatchers.Main via withContext, serializing onDelta
        // and the pacer on the single UI thread — they interleave only at suspension
        // points and share `accumulated`/`revealed` without a lock. The pacer's only
        // suspension is delay(); its tick body never yields, so accumulated is stable
        // while it is read. The per-delta work pinned here is light (a StringBuilder
        // append + a small StateFlow update); the turn's heavy work stays off Main.
        val revealTickMs = 33L
        val revealDrainDivisor = 4
        val revealMinChars = 2
        var revealed = 0
        // Live progress: the gateway's tool/thinking SSE frames become transient
        // TOOL_EXECUTING rows so the waiting chip narrates what the agent is
        // doing ("메일 확인 중") instead of cycling generic spinner text.
        val progress = TurnProgress(_chatHistory, scope)
        val reply = try {
            withContext(Dispatchers.Main) {
                val pacer = launch {
                    while (isActive) {
                        if (revealed < accumulated.length) {
                            val backlog = accumulated.length - revealed
                            val step = maxOf(revealMinChars, backlog / revealDrainDivisor)
                            revealed = minOf(accumulated.length, revealed + step)
                            replaceAssistant(accumulated.toString().take(revealed), null)
                        }
                        delay(revealTickMs)
                    }
                }
                try {
                    sendStreaming(
                        sendText,
                        onTool = progress::onTool,
                        onThinking = progress::onThinking,
                    ) { delta ->
                        progress.onDelta()
                        accumulated.append(delta)
                    }
                } finally {
                    pacer.cancel()
                }
            }
        } catch (cancel: CancellationException) {
            // The turn's coroutine died (topic switch, VM teardown). Never leave
            // the placeholder as a phantom empty bubble: keep what streamed, or
            // drop the row — the transcript reconcile on the next events-stream
            // (re)connect restores the canonical answer if the server finished.
            settlePlaceholderOnAbort(assistantId, accumulated.toString())
            askActive = false
            throw cancel
        } catch (e: Exception) {
            // Mid-stream failure. On mobile the socket often dies HALF-OPEN: the
            // gateway never notices, keeps running the turn, and persists an
            // answer nobody received. The transcript is the canonical record —
            // reconcile with it BEFORE considering a re-send, which would
            // double-generate the answer for a message that did arrive.
            val recovered = try {
                recoverTurnFromTranscript(sessionKeyAtSend, sendText)
            } catch (cancel: CancellationException) {
                settlePlaceholderOnAbort(assistantId, accumulated.toString())
                askActive = false
                throw cancel
            }
            recovered
                ?: if (accumulated.isEmpty()) {
                    // Never reached the gateway (or an old gateway without the
                    // stream endpoint) — a blocking re-send is safe.
                    runCatching { send(sendText) }
                        .getOrElse { GatewayReply("⚠️ ${it.message ?: "gateway request failed"}", ok = false) }
                } else {
                    // Keep the partial answer, but the turn still FAILED (stream broke
                    // mid-answer) — report ok=false so the queue never auto-fires after it.
                    GatewayReply(text = accumulated.toString(), ok = false)
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
        askActive = false
        return reply.ok
    }

    // Never leave ask()'s optimistic placeholder as a phantom empty bubble when
    // the turn dies mid-flight: keep whatever streamed, or drop the row.
    private fun settlePlaceholderOnAbort(assistantId: String, partial: String) {
        _chatHistory.update { list ->
            if (partial.isBlank()) {
                list.filter { it.id != assistantId }
            } else {
                list.map { if (it.id == assistantId) it.copy(content = partial) else it }
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
        // A fresh independent conversation branching off the home (client:main:<uuid>).
        switchSession(newSessionKey())
    }

    internal fun newSessionKey(): String = "client:main:${Uuid.random()}"

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

            // Legacy 챗봇 sessions (flat chat:<uuid>, retired workspace) — just
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
        if (sessionKey == "client:main") {
            scope.launch { loadTranscriptGuarded("client:main") }
        } else {
            // Not on the work home — raise the in-app unread badge so the banner
            // points the user at the 업무 topic.
            _hasUnreadWorkReport.value = true
        }
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
            return GatewayReply("⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요.", ok = false)
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
            GatewayReply("⚠️ 게이트웨이 오류", ok = false)
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
            return GatewayReply("⚠️ Deneb 클라이언트 토큰이 설정되지 않았습니다. 게이트웨이에서 deneb-client-token을 생성해 설정하세요.", ok = false)
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
            readSseLines(channel) { line ->
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
    internal data class RpcRequest(val id: String, val method: String, val params: SendParams)

    @Serializable
    internal data class SendParams(
        val message: String,
        val sessionKey: String? = null,
    )

    @Serializable
    internal data class RpcResponse(val ok: Boolean = false, val payload: SendPayload? = null)

    @Serializable
    internal data class SendPayload(
        val text: String = "",
        val model: String = "",
        val sessionKey: String = "",
        // True when the gateway's model fallback chain fired (main → fallback);
        // `model` is then the model that actually answered. Surfaced as a badge.
        val fellBack: Boolean = false,
    )

    /**
     * Internal result of one gateway chat turn (text + which model answered).
     * [ok] is false when the turn FAILED and [text] is an ⚠️ error notice (or a
     * partial answer cut off mid-stream) rather than a real reply — [ask] renders
     * it as a bubble but reports the failure to its caller, so the ViewModel never
     * auto-fires queued messages after a failed turn.
     */
    internal data class GatewayReply(
        val text: String,
        val model: String = "",
        val fellBack: Boolean = false,
        val ok: Boolean = true,
    )

    // SSE frame payloads from POST /api/v1/miniapp/chat/stream.
    @Serializable
    private data class DeltaEvent(val delta: String = "")

    // preview: chip-sized tail of the live reasoning text the gateway condenses
    // from the model's thinking stream; empty on bare liveness pulses.
    @Serializable
    private data class ThinkingEvent(val preview: String = "")

    @Serializable
    private data class DoneEvent(val text: String = "", val model: String = "", val fellBack: Boolean = false)

    @Serializable
    private data class ErrorEvent(val error: String = "")

    // A proactive frame off the events stream. kind="phone_action" marks a command
    // the gateway's phone_write tool dispatched (server: pushKindPhoneAction) for
    // in-app Intent execution — data carries "action" plus its args (url/package/
    // number/text/to) — rather than a {title, body} notification to surface. ref
    // carries the dispatch id: the gateway waits briefly on it, so the executor
    // reports the execution result back via a phone_action_result ingest event.
    @Serializable
    internal data class PushEvent(
        val title: String = "",
        val body: String = "",
        val kind: String = "",
        val ref: String = "",
        val data: Map<String, String> = emptyMap(),
    )

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

        // Device class for the gateway's per-mobile FCM-fallback predicate
        // (server clientKindFromHeader). "mobile" so a connected desktop never
        // suppresses this phone's background push.
        const val CLIENT_KIND_HEADER = "X-Deneb-Client-Kind"
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

        // Stream-failure recovery (recoverTurnFromTranscript): how long to keep
        // polling the transcript for the answer of a turn whose SSE died — the
        // common half-open case resolves in seconds, but a tool-heavy turn can
        // run minutes — and the poll cadence.
        const val STREAM_RECOVERY_BUDGET_MS = 90_000L
        const val STREAM_RECOVERY_POLL_MS = 3_000L

        // How long a fetched calendar month is served from the client cache before
        // a re-open refetches. Short enough that an event added elsewhere surfaces
        // soon (and pull-to-refresh / edits force immediately), long enough that
        // rapid tab-switching back to the calendar is instant.
        val CAL_RANGE_TTL = 120.seconds

        // Minimum gap between background warms of the calendar + mail caches (see
        // warmHomeCachesThrottled). Frequent enough that a backgrounded app keeps a
        // recent offline glance, sparse enough that bursty SSE frames don't re-fetch
        // the home trio on every proactive event. User-driven refreshes (screen entry,
        // pull-to-refresh) are independent and always immediate.
        val HOME_WARM_INTERVAL = 10.minutes

        // Minimum gap between app-usage-digest forwards (see maybeForwardUsageDigest).
        // A digest summarizes a multi-hour window, so forwarding more often would just
        // re-report the same rhythm; ~6h keeps the gateway's ambient work-context fresh
        // without creating proactive alerts.
        val USAGE_FORWARD_INTERVAL = 6.hours

        // Minimum gap between location forwards (see maybeForwardLocation). Frequent
        // enough that phone_read has a recent fix, sparse enough to spare the battery;
        // the gateway treats a cache older than ~30min as stale and does a live read.
        val LOCATION_FORWARD_INTERVAL = 10.minutes
    }
}
