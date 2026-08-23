package ai.deneb.deneb

import ai.deneb.DenebLog
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
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.serialization.kotlinx.json.json
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.concurrent.Volatile
import kotlin.time.Duration.Companion.hours
import kotlin.time.Duration.Companion.minutes
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
 * This file owns the long-lived repository state and conversation lifecycle.
 * Chat turn orchestration and RPC/SSE transport live in focused sibling files. The
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
class DenebGatewayClient private constructor(
    // internal (not private) so the facade's extension files (sessions, admin,
    // patch notes) can read persisted settings.
    internal val appSettings: AppSettings,
    private val smsDraftStore: SmsDraftStore,
    private val smsSender: SmsSender,
    injectedHttp: (() -> io.ktor.client.HttpClient)?,
    // Injectable so tests never touch (or wipe) the machine's real mirror
    // files; null = the platform app-files directory.
    wikiMirrorFiles: WikiMirrorFiles?,
) : DataRepository {

    constructor(appSettings: AppSettings, smsDraftStore: SmsDraftStore, smsSender: SmsSender) :
        this(appSettings, smsDraftStore, smsSender, null, null)

    internal constructor(
        appSettings: AppSettings,
        smsDraftStore: SmsDraftStore,
        smsSender: SmsSender,
        http: io.ktor.client.HttpClient,
        wikiMirrorFiles: WikiMirrorFiles? = null,
    ) : this(appSettings, smsDraftStore, smsSender, { http }, wikiMirrorFiles)

    internal val jsonCodec = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    internal val http = injectedHttp?.invoke() ?: httpClient {
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
    //
    // The handler is not optional. Nothing awaits these launches, so on Android an
    // exception escaping one is an uncaught exception on a process-wide scope —
    // i.e. a crash — and the work here is not only RPC (which callRpc already
    // contains): it decodes cached transcripts and writes settings, where a
    // corrupt blob or a full disk would otherwise take the app down while the user
    // was just scrolling. Log and keep the other children alive (SupervisorJob).
    internal val scope = CoroutineScope(
        SupervisorJob() +
            Dispatchers.Default +
            CoroutineExceptionHandler { _, error ->
                DenebLog.error("DenebGatewayClient", "background task failed", error)
            },
    )

    internal val _chatHistory = MutableStateFlow<List<History>>(emptyList())
    override val chatHistory: StateFlow<List<History>> = _chatHistory

    // Guards _chatHistory against a background transcript load clobbering an
    // in-flight optimistic send. A cold-start share (onCreate, before the chat
    // UI exists) appends its message and starts streaming while a topic
    // auto-select is still fetching that topic's transcript; without this gate
    // the late fetch overwrote both the shared message and its streaming reply,
    // so the share showed NO response until the user sent another message.
    // ask() and a successful steer() bump the epoch when they append; loadTranscriptGuarded
    // only installs its result when the epoch is unchanged, making the two order-independent.
    internal val historyGate = Mutex()

    // True while ask() drives a turn. Background transcript reconciles (events
    // stream reconnect) must not touch the view then — the live stream, or its
    // stream-failure recovery, owns it. Backed by a StateFlow (replacing the old
    // @Volatile var — StateFlow.value reads are equally atomic, worst race is
    // still one skipped reconcile) so the Android BackgroundConnectionPolicy can
    // observe it and hold the foreground service open until an in-flight turn
    // completes (M1 active-stream exception, battery doc §3.1).
    private val _chatTurnActive = MutableStateFlow(false)
    val chatTurnActive: StateFlow<Boolean> = _chatTurnActive.asStateFlow()
    internal var askActive: Boolean
        get() = _chatTurnActive.value
        set(value) {
            _chatTurnActive.value = value
        }

    // Set when an in-flight turn (stream or its transcript recovery) is
    // CANCELLED — typically by the user re-sending — while the server turn may
    // still be running. The stranded answer lands only in the transcript, so
    // the next completed ask reconciles the view against it (production
    // 2026-08-01: a re-send cancelled recovery seconds before it would have
    // installed the finished reply, leaving it permanently invisible).
    internal var reconcileAfterTurn = false

    // Whether the gateway confirmed it can actually DELIVER push (FCM sender
    // configured server-side), from the last miniapp.push.register response.
    // Persisted so a cold start keeps the last known answer while offline.
    // BackgroundConnectionPolicy treats false as "FCM handoff unsafe" and keeps
    // background SSE alive instead of dozing (battery doc §3.1 acked-token /
    // server-credential gate) — losing notifications is worse than losing Doze.
    internal val _fcmDeliveryReady = MutableStateFlow(appSettings.settings.getBoolean(KEY_FCM_DELIVERY, false))
    val fcmDeliveryReady: StateFlow<Boolean> = _fcmDeliveryReady.asStateFlow()
    internal var historyEpoch = 0L
    internal val nativeSyncGate = Mutex()
    internal var nativeSyncCursor = appSettings.settings.getLong(KEY_SYNC_CURSOR, 0L)

    private val _savedConversations = MutableStateFlow<List<Conversation>>(emptyList())
    override val savedConversations: StateFlow<List<Conversation>> = _savedConversations

    // Drawer paging: `total` from the gateway minus what it has handed over so
    // far. Conversations are no longer GC'd server-side (#4353), so the list
    // outgrows one page and a page-only drawer would hide the rest for good.
    private val _hasMoreConversations = MutableStateFlow(false)
    override val hasMoreConversations: StateFlow<Boolean> = _hasMoreConversations
    private var serverRowsLoaded = 0

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

    // Per-conversation model overrides from miniapp.sessions.recent / models.set.
    // The chat-input switcher reads this so a picker change does not retarget
    // every other session (or the settings-tab default).
    internal val _sessionModels = MutableStateFlow<Map<String, String>>(emptyMap())
    val sessionModels: StateFlow<Map<String, String>> = _sessionModels

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

    // Recent 결재 list (folder=total). Detail act patches this so the list
    // recomposes without a refetch when the operator returns.
    internal val _denebApprovals = MutableStateFlow<List<ai.deneb.deneb.generated.GroupwareApprovalRow>>(emptyList())
    val denebApprovals: StateFlow<List<ai.deneb.deneb.generated.GroupwareApprovalRow>> = _denebApprovals

    // afterDocId cursor for load-more; null when no further page.
    internal val _denebApprovalsNextAfter = MutableStateFlow<String?>(null)
    val denebApprovalsNextAfter: StateFlow<String?> = _denebApprovalsNextAfter

    // True once the first network (or seed) paint has landed — distinguishes
    // "still loading" from "genuinely empty".
    internal val _denebApprovalsReady = MutableStateFlow(false)
    val denebApprovalsReady: StateFlow<Boolean> = _denebApprovalsReady

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

    // Session caches for the 더보기 section list fetches (카테고리·사람·연락처·일기·
    // 노트북·현황·조직도), the wiki browse loop, and the calendar month grid —
    // see DenebClientSessionCache.kt. Instance-scoped so a fresh client (test
    // harness) starts cold; disk snapshots are owner-fingerprinted per account.
    internal val sectionCaches = SectionCaches(appSettings) { mailCacheOwner(gatewayUrl, clientToken) }

    // Offline wiki mirror (WikiMirror.kt): the whole corpus on disk, bulk-seeded
    // from miniapp.memory.mirror and kept current by wiki.changed sync events.
    // The 위키 read paths fall back here when the network fetch fails.
    private val mirrorFiles = wikiMirrorFiles ?: platformWikiMirrorFiles()
    internal val wikiMirror = WikiMirrorStore(mirrorFiles) { mailCacheOwner(gatewayUrl, clientToken) }
    internal var wikiMirrorRefreshInFlight = false

    // Offline diary mirror (DiaryMirror.kt): shares the wiki mirror's storage
    // directory (distinct file), so test injection isolates both at once.
    // Offline 검색 falls back here for the 일기 section.
    internal val diaryMirror = DiaryMirrorStore(mirrorFiles) { mailCacheOwner(gatewayUrl, clientToken) }
    internal var diaryMirrorRefreshInFlight = false

    // Attempt throttle for the mirror's full refresh (same shape as
    // lastHomeWarm): bounds retry pressure when the pull keeps failing, and
    // lets sync contract tests pre-arm it to keep request sequences exact.
    internal var lastWikiMirrorRefresh: TimeSource.Monotonic.ValueTimeMark? = null
    internal var lastDiaryMirrorRefresh: TimeSource.Monotonic.ValueTimeMark? = null

    // Calendar month cache (range-key → when-fetched + events). The calendar
    // screen's own cache is composition-scoped, so every tab switch back to the
    // calendar re-hit Google for the visible month + both neighbors (~270ms each).
    // This client-level cache survives navigation (and, disk-backed, restarts —
    // the grid paints the last-known dots instantly on cold start); a short TTL
    // bounds staleness and force=true (pull-to-refresh, after an edit) bypasses
    // it. Accessed only from the screen's Main-scoped coroutines.

    /** Last-known events for [key] regardless of age — the cold-start stale paint. */
    internal fun peekCalendarRange(key: String): List<CalendarEvent>? = sectionCaches.calendarRanges.peek(key)

    // Native-client handshake snapshot: gateway version, active model, and
    // feature flags exposed by miniapp.client.hello.
    internal val _clientStatus = MutableStateFlow<ClientStatus?>(null)
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
    data class ProactiveNotification(
        val title: String,
        val body: String,
        val kind: String = "workfeed",
        val ref: String = "",
        // Trust Inbox: the card's approval:* action ids, when present. Android
        // turns these into tray 승인/거절 buttons so a decision settles without
        // opening the app; null when the card carries no approval actions.
        val approveActionId: String? = null,
        val rejectActionId: String? = null,
    )

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
        private set
    private val _currentConversationId = MutableStateFlow<String?>(sessionKey)
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
        _denebApprovals.value = emptyList()
        _denebApprovalsNextAfter.value = null
        _denebApprovalsReady.value = false
        invalidateApprovalsListCache()
        _denebWorkFeed.value = emptyList()
        _workFeedLoaded.value = false
        _hasUnreadWorkReport.value = false
        _hasUnreadHeartbeat.value = false
        _denebMemories.value = emptyList()
        _denebScheduledTasks.value = emptyList()
        _denebCalendar.value = emptyList()
        _denebCalProposals.value = emptyList()
        sectionCaches.clearAll()
        // The mirrors hold account A's whole wiki/diary on disk; wipe memory
        // synchronously (offline fallbacks consult the hot maps) then delete
        // the files on a worker.
        wikiMirror.evictMemoryForCredentialSwitch()
        diaryMirror.evictMemoryForCredentialSwitch()
        scope.launch { wikiMirror.clear() }
        scope.launch { diaryMirror.clear() }
        _denebModels.value = emptyList()
        _sessionModels.value = emptyMap()
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

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?): Boolean = askGateway(question, uiSubmission)

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
            _savedConversations.value = fresh.conversations
            serverRowsLoaded = fresh.serverRows
            _hasMoreConversations.value = fresh.serverRows < fresh.total
        }
    }

    override fun loadMoreConversations() {
        scope.launch {
            val epoch = credEpoch
            val loaded = _savedConversations.value
            // Offset by what the SERVER has handed over so far. The 업무 home row is
            // synthesized locally when the first page lacks it, so counting the
            // rendered list would skip a real conversation at the page boundary.
            val next = fetchRecentSessions(offset = serverRowsLoaded) ?: return@launch
            if (epoch != credEpoch) return@launch
            serverRowsLoaded += next.serverRows
            val known = loaded.mapTo(mutableSetOf()) { it.id }
            _savedConversations.value = loaded + next.conversations.filterNot { it.id in known }
            _hasMoreConversations.value = serverRowsLoaded < next.total && next.serverRows > 0
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

    override suspend fun renameConversation(id: String, label: String) {
        val trimmed = label.trim()
        if (id.isBlank() || trimmed.isEmpty()) return
        val out = callRpc<JsonObject>(
            "miniapp.sessions.rename",
            buildJsonObject {
                put("sessionKey", id)
                put("label", trimmed)
            },
        ) ?: return
        val applied = out["label"]?.jsonPrimitive?.contentOrNull ?: trimmed
        _savedConversations.update { list ->
            list.map { if (it.id == id) it.copy(title = applied) else it }
        }
    }

    override suspend fun steer(note: String): Boolean {
        val trimmed = note.trim()
        if (trimmed.isEmpty()) return false
        val out = callRpc<JsonObject>(
            "miniapp.chat.steer",
            buildJsonObject {
                put("sessionKey", sessionKey)
                put("note", trimmed)
            },
        ) ?: return false
        if (out["steered"]?.jsonPrimitive?.booleanOrNull != true) return false
        historyGate.withLock {
            historyEpoch++
            _chatHistory.update { it.withSteerUserNote(trimmed) }
        }
        return true
    }

    // --- Memory screen → Deneb wiki (read-only browser) ---------------------
    // Wiki pages ([denebMemories]) and Deneb crons ([denebScheduledTasks]) are
    // surfaced to their screens through the concrete StateFlows + refresh
    // extensions in DenebClientMemory.kt / DenebClientAdmin.kt (refreshMemories,
    // refreshScheduledTasks, removeCron) — not the DataRepository interface, which
    // no longer carries on-device memory/scheduling members.

    internal fun switchSession(key: String) {
        sessionKey = key
        _currentConversationId.value = key
        // Remember this as the active session so a restart restores it.
        appSettings.setLastSession(key)
    }

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
        const val KEY_FCM_DELIVERY = "deneb.fcmDeliveryReady"

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
        // keepalive comment every 15s (chatStreamKeepaliveInterval), so 45s =
        // three consecutive missed keepalives — a high-confidence dead socket,
        // not a slow turn. The previous 120s window sat right at user patience:
        // production 2026-08-01, a half-open socket kept "깊이 생각 중" up for
        // ~2min with the finished answer stranded in the transcript, and the
        // user re-sent at 2m22s — cancelling the recovery poll seconds before
        // it would have installed the reply ("채팅 안 보임"). Tripping at 45s
        // hands the zombie stream to recoverTurnFromTranscript (which shows the
        // resuming chip and polls up to STREAM_RECOVERY_MAX_MS) well inside
        // patience. A transient network blip that trips this is harmless: the
        // same recovery path re-installs the canonical transcript.
        const val STREAM_SOCKET_TIMEOUT_MS = 45_000L

        // Events-stream idle cap. Its server keepalive cadence is 30s
        // (clientEventsKeepaliveInterval) — twice the chat stream's — so the
        // same three-missed-keepalives discipline lands at 90s. Sharing the
        // chat stream's 45s here would be only 1.5 intervals: one jittered
        // keepalive would false-drop a healthy events stream into reconnect
        // churn (radio wakeups) for no recovery benefit.
        const val EVENTS_SOCKET_TIMEOUT_MS = 90_000L

        // Stream-failure recovery (recoverTurnFromTranscript): how long to keep
        // polling the transcript for the answer of a turn whose SSE died. The
        // short budget bounds the no-signal case (transcript unreachable / turn
        // never landed) so a truly-lost turn can't hang the resend path.
        const val STREAM_RECOVERY_BUDGET_MS = 90_000L

        // Once the transcript confirms the turn is still running server-side, poll
        // through the server's interactive turn deadline (30m) plus one poll interval
        // so a late answer is not missed while the detached run is still finishing.
        // A tool-heavy turn (multiple wiki/mail lookups) routinely outlives the short
        // budget; giving up early froze the client on the streamed preamble while the
        // finished answer sat in the transcript unseen.
        // Must stay >= gateway-go chatport.InteractiveTurnDeadline (30m).
        const val STREAM_RECOVERY_MAX_MS = 1_803_000L
        const val STREAM_RECOVERY_POLL_MS = 3_000L

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

/**
 * Inserts a mid-turn steer as a user row just before the live assistant so the
 * bubble order matches the transcript (original user → steer → answer).
 */
internal fun List<History>.withSteerUserNote(note: String): List<History> {
    val trimmed = note.trim()
    if (trimmed.isEmpty()) return this
    if (any { it.role == History.Role.USER && isSameSteerUserContent(it.content, trimmed) }) {
        return this
    }
    val row = History(role = History.Role.USER, content = trimmed)
    val assistantAt = indexOfLast { it.role == History.Role.ASSISTANT }
    return if (assistantAt >= 0) {
        take(assistantAt) + row + drop(assistantAt)
    } else {
        this + row
    }
}

internal fun isSameSteerUserContent(content: String, note: String): Boolean {
    val c = content.trim()
    val n = note.trim()
    if (c == n) return true
    val close = c.indexOf("] ")
    return close in 1..40 && c.startsWith("[") && c.substring(close + 2) == n
}
