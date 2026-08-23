package ai.deneb.data

import ai.deneb.ui.chat.History
import io.github.vinceglb.filekit.PlatformFile
import kotlinx.coroutines.flow.StateFlow

/**
 * The chat surface the UI talks to. The sole production implementation is
 * [ai.deneb.deneb.DenebGatewayClient], which drives the Deneb gateway's
 * `miniapp.*` RPC surface; [ai.deneb.testutil.FakeDataRepository] backs the
 * ViewModel tests.
 *
 * Scope: only what `ChatViewModel`, `TaskScheduler`, and `MainActivity` reach
 * through this interface type. Everything else the gateway client exposes —
 * mail, calendar, wiki, models, fleet, work-feed, capture — lives as concrete
 * members / extension functions on `DenebGatewayClient` and the screens take it
 * by concrete type. The legacy on-device (cloud-direct) provider surface that
 * used to live here (configured services, per-instance keys, MCP, soul, on-device
 * memory/scheduling/email/SMS settings, `askWithTools`/`askSilently`) was removed
 * along with `RemoteDataRepository`.
 */
interface DataRepository {
    val chatHistory: StateFlow<List<History>>
    val currentConversationId: StateFlow<String?>
    val fallbackStatus: StateFlow<FallbackStatus?>

    /**
     * Runs one chat turn. Returns `true` when the turn completed successfully,
     * `false` when it failed but the failure was surfaced in-transcript instead of
     * thrown — the gateway client converts stream/RPC failures into an ⚠️ error
     * bubble and returns normally. Callers gate follow-up side effects on this:
     * queued messages must never auto-fire after a failed turn (ChatViewModel).
     */
    suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission? = null): Boolean
    fun clearHistory()
    fun supportedFileExtensions(): List<String>

    // Conversation management
    val savedConversations: StateFlow<List<Conversation>>

    /**
     * Whether the gateway holds conversations the drawer has not pulled in yet.
     * Conversations are no longer garbage-collected server-side, so the list
     * outgrows one page — without paging the older ones would be unreachable
     * even though their transcripts are intact.
     */
    val hasMoreConversations: StateFlow<Boolean>
    fun loadConversations()

    /** Appends the next page to [savedConversations]. */
    fun loadMoreConversations()
    fun loadConversation(id: String)

    /** True when the gateway accepted the delete; false when it refused or was unreachable. */
    suspend fun deleteConversation(id: String): Boolean

    /**
     * Pins a drawer label so the auto-titler will not overwrite it.
     * True when the gateway accepted the rename; false when it refused or was unreachable.
     */
    suspend fun renameConversation(id: String, label: String): Boolean

    /**
     * Folds a mid-turn note into the active reply. Returns `true` when the
     * gateway accepted it; `false` means the caller should queue instead.
     */
    suspend fun steer(note: String): Boolean
    fun startNewChat()
    fun popLastExchange()
    fun truncateFrom(messageId: String)
    fun restoreCurrentConversation()

    // SMS drafts (FOSS-only on Android; the gateway proposes a draft, the user
    // approves it via the in-app banner, and the phone sends it). Read and send
    // are independent opt-ins with separate runtime permissions.
    val smsDrafts: StateFlow<List<SmsDraft>>
    suspend fun sendSmsDraft(draftId: String): Boolean
    suspend fun discardSmsDraft(draftId: String)

    // Heartbeat notification
    val hasUnreadHeartbeat: StateFlow<Boolean>
    fun clearUnreadHeartbeat()

    /**
     * Pulse that fires when the user taps a heartbeat push notification while the app is
     * not already on the heartbeat conversation. `true` means "load the heartbeat
     * conversation now, then call [consumeOpenHeartbeatRequest]". Set by MainActivity
     * (Android push tap), collected by `ChatViewModel` in its init block.
     */
    val openHeartbeatRequested: StateFlow<Boolean>
    fun requestOpenHeartbeat()
    fun consumeOpenHeartbeatRequest()

    /**
     * Pulse that fires when the user taps a proactive-report push notification
     * (morning-letter, email-analysis). Those reports are mirrored to the 업무
     * (General) topic, not the heartbeat conversation, so this opens the work
     * topic instead. Set by MainActivity, collected by `ChatViewModel`.
     */
    val openWorkTopicRequested: StateFlow<Boolean>
    fun requestOpenWorkTopic()
    fun consumeOpenWorkTopicRequest()

    /**
     * Unread badge for a proactive report (morning-letter, mail-analysis) that
     * landed in the 업무 (client:main) topic while the user was looking at a
     * different conversation. Surfaced as an in-app banner; tapping it opens the
     * work topic. Distinct from [hasUnreadHeartbeat] (the heartbeat conversation).
     */
    val hasUnreadWorkReport: StateFlow<Boolean>
    fun clearUnreadWorkReport()

    /**
     * Called by the scheduler when a proactive-report push arrives while the app
     * is foregrounded (so no system notification fires). Refreshes the home
     * transcript if it is the current view, or raises the unread badge otherwise.
     */
    fun onProactiveReportForeground()
}
