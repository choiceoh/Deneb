package ai.deneb.testutil

import ai.deneb.data.Conversation
import ai.deneb.data.DataRepository
import ai.deneb.data.FallbackStatus
import ai.deneb.data.SmsDraft
import ai.deneb.data.UiSubmission
import ai.deneb.ui.chat.History
import io.github.vinceglb.filekit.PlatformFile
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update

/**
 * Test double for the narrowed [DataRepository] — the chat surface `ChatViewModel`
 * reaches through the interface type. (The production gateway-specific behaviors live
 * on the concrete `DenebGatewayClient` and aren't part of this interface.)
 */
class FakeDataRepository : DataRepository {

    override val chatHistory: MutableStateFlow<List<History>> = MutableStateFlow(emptyList())
    override val currentConversationId: MutableStateFlow<String?> = MutableStateFlow(null)
    override val fallbackStatus: MutableStateFlow<FallbackStatus?> = MutableStateFlow(null)
    override val savedConversations: MutableStateFlow<List<Conversation>> = MutableStateFlow(emptyList())
    override val hasMoreConversations: MutableStateFlow<Boolean> = MutableStateFlow(false)
    var loadMoreCalls: Int = 0
        private set

    val askCalls = mutableListOf<Pair<String?, List<PlatformFile>>>()
    var clearHistoryCalls = 0
    var askException: Exception? = null

    /**
     * Return value of [ask]. `false` mimics the gateway client surfacing a turn
     * failure as an in-transcript ⚠️ bubble instead of throwing (the ViewModel
     * must then NOT auto-send queued messages).
     */
    var askResult: Boolean = true

    /**
     * When non-null, [ask] suspends on this gate before doing any work. Tests can use this
     * to keep an in-flight ask in progress while inspecting state (e.g., to verify
     * concurrent ask prevention or to test cancel behavior).
     */
    var askGate: CompletableDeferred<Unit>? = null

    override suspend fun ask(question: String?, files: List<PlatformFile>, uiSubmission: UiSubmission?): Boolean {
        askCalls.add(question to files)
        askGate?.await()
        askException?.let { throw it }
        if (question != null) {
            chatHistory.update { history ->
                history + History(role = History.Role.USER, content = question, uiSubmission = uiSubmission)
            }
        }
        chatHistory.update { history ->
            history + History(role = History.Role.ASSISTANT, content = "Test response")
        }
        return askResult
    }

    override fun clearHistory() {
        clearHistoryCalls++
        chatHistory.value = emptyList()
    }

    var fileAttachmentSupported = true

    override fun supportedFileExtensions(): List<String> = if (fileAttachmentSupported) listOf("txt", "pdf", "png") else emptyList()

    // Conversation management
    override fun loadMoreConversations() {
        loadMoreCalls++
    }

    override fun loadConversations() {
        // No-op in tests
    }

    override fun loadConversation(id: String) {
        val conversation = savedConversations.value.find { it.id == id } ?: return
        currentConversationId.value = id
        chatHistory.value = conversation.messages.map { m ->
            History(
                id = m.id,
                role = when (m.role) {
                    "user" -> History.Role.USER
                    "tool" -> History.Role.TOOL
                    else -> History.Role.ASSISTANT
                },
                content = m.content,
            )
        }
    }

    /** Flip to false to exercise the refused/unreachable branch. */
    var deleteConversationSucceeds: Boolean = true

    override suspend fun deleteConversation(id: String): Boolean {
        if (!deleteConversationSucceeds) return false
        if (currentConversationId.value == id) {
            currentConversationId.value = null
            chatHistory.value = emptyList()
        }
        savedConversations.update { it.filter { c -> c.id != id } }
        return true
    }

    var abortTurnCalls = 0

    override suspend fun abortTurn(): Boolean {
        abortTurnCalls++
        return true
    }

    val renameCalls = mutableListOf<Pair<String, String>>()

    /** Flip to false to exercise the refused/unreachable branch. */
    var renameConversationSucceeds: Boolean = true

    override suspend fun renameConversation(id: String, label: String): Boolean {
        renameCalls.add(id to label)
        if (!renameConversationSucceeds) return false
        savedConversations.update { list ->
            list.map { if (it.id == id) it.copy(title = label) else it }
        }
        return true
    }

    override suspend fun pinConversation(id: String, pinned: Boolean) {
        savedConversations.update { list ->
            list.map { if (it.id == id) it.copy(pinned = pinned) else it }
        }
    }

    override suspend fun resetConversationModel(id: String) {
        savedConversations.update { list ->
            list.map { if (it.id == id) it.copy(model = "") else it }
        }
    }

    override suspend fun searchConversations(query: String): List<Conversation> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        return savedConversations.value.filter {
            it.title.contains(q, ignoreCase = true) || it.id.contains(q, ignoreCase = true)
        }
    }

    /**
     * Default false so existing queue-while-loading tests keep the after-turn
     * path. Set true to exercise mid-turn steer.
     */
    var steerResult: Boolean = false
    val steerCalls = mutableListOf<String>()

    override suspend fun steer(note: String): Boolean {
        steerCalls.add(note)
        if (steerResult) {
            chatHistory.update { it + History(role = History.Role.USER, content = note) }
        }
        return steerResult
    }

    override fun startNewChat() {
        currentConversationId.value = null
        chatHistory.value = emptyList()
    }

    override fun popLastExchange() {
        chatHistory.update { history ->
            val lastUserIndex = history.indexOfLast { it.role == History.Role.USER }
            if (lastUserIndex >= 0) history.subList(0, lastUserIndex) else history
        }
    }

    override fun truncateFrom(messageId: String) {
        chatHistory.update { history ->
            val index = history.indexOfFirst { it.id == messageId }
            if (index >= 0) history.subList(0, index) else history
        }
    }

    override fun restoreCurrentConversation() {
        // No-op in tests
    }

    // SMS drafts
    private val _smsDrafts = MutableStateFlow(emptyList<SmsDraft>())
    override val smsDrafts: StateFlow<List<SmsDraft>> = _smsDrafts
    override suspend fun sendSmsDraft(draftId: String): Boolean = true
    override suspend fun discardSmsDraft(draftId: String) {
        _smsDrafts.value = _smsDrafts.value.filterNot { it.id == draftId }
    }

    // Heartbeat / work-report notification pulses
    override val hasUnreadHeartbeat: MutableStateFlow<Boolean> = MutableStateFlow(false)
    override fun clearUnreadHeartbeat() {
        hasUnreadHeartbeat.value = false
    }

    override val openHeartbeatRequested: MutableStateFlow<Boolean> = MutableStateFlow(false)
    override fun requestOpenHeartbeat() {
        openHeartbeatRequested.value = true
    }
    override fun consumeOpenHeartbeatRequest() {
        openHeartbeatRequested.value = false
    }

    override val openWorkTopicRequested: MutableStateFlow<Boolean> = MutableStateFlow(false)
    override fun requestOpenWorkTopic() {
        openWorkTopicRequested.value = true
    }
    override fun consumeOpenWorkTopicRequest() {
        openWorkTopicRequested.value = false
    }

    override val hasUnreadWorkReport: MutableStateFlow<Boolean> = MutableStateFlow(false)
    override fun clearUnreadWorkReport() {
        hasUnreadWorkReport.value = false
    }
    override fun onProactiveReportForeground() {
        hasUnreadWorkReport.value = true
    }
}
