package ai.deneb.ui.chat

import ai.deneb.data.Conversation
import ai.deneb.data.DataRepository
import ai.deneb.data.TaskScheduler
import ai.deneb.data.UiSubmission
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.denebServiceEntries
import ai.deneb.deneb.refreshModelsAsync
import ai.deneb.deneb.selectDenebModelInstance
import ai.deneb.getBackgroundDispatcher
import ai.deneb.network.toUiError
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.conversation_untitled
import deneb.composeapp.generated.resources.error_unsupported_file_type
import io.github.vinceglb.filekit.PlatformFile
import io.github.vinceglb.filekit.extension
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import org.jetbrains.compose.resources.getString
import kotlin.coroutines.CoroutineContext
import kotlin.time.Duration.Companion.seconds

class ChatViewModel(
    private val dataRepository: DataRepository,
    private val taskScheduler: TaskScheduler,
    private val backgroundDispatcher: CoroutineContext = getBackgroundDispatcher(),
) : ViewModel() {

    private val actions = ChatActions(
        ask = ::ask,
        retry = ::retry,
        toggleSpeechOutput = ::toggleSpeechOutput,
        toggleRecall = ::toggleRecall,
        clearHistory = ::clearHistory,
        setIsSpeaking = ::setIsSpeaking,
        addFile = ::addFile,
        removeFile = ::removeFile,
        startNewChat = ::startNewChat,
        regenerate = ::regenerate,
        cancel = ::cancel,
        selectService = ::selectService,
        loadConversation = ::loadConversation,
        deleteConversation = ::deleteConversation,
        clearUnreadHeartbeat = ::clearUnreadHeartbeat,
        clearUnreadWorkReport = ::clearUnreadWorkReport,
        openWorkReport = ::openWorkReport,
        openWorkFeedItem = ::openWorkFeedItem,
        consumePendingScroll = ::consumePendingScroll,
        runWorkFeedAction = ::runWorkFeedAction,
        clearSnackbar = ::clearSnackbar,
        undoDeleteConversation = ::undoDeleteConversation,
        submitUiCallback = ::submitUiCallback,
        resubmit = ::resubmit,
        sendSmsDraft = ::sendSmsDraft,
        discardSmsDraft = ::discardSmsDraft,
        refreshConversations = { dataRepository.loadConversations() },
    )
    private var currentJob: Job? = null
    private var pendingConversationDeleteJob: Job? = null
    private val _state = MutableStateFlow(
        ChatUiState(
            actions = actions,
        ),
    )

    init {
        // Seed the memory-recall toggle from the persisted setting (default on).
        _state.update { it.copy(recallEnabled = dataRepository.isRecallEnabled()) }
        updateAvailableServices()
        // Deneb: the chat-input model switcher lists gateway models; rebuild it
        // whenever the model registry changes (after a switch or on first load).
        if (dataRepository is DenebGatewayClient) {
            dataRepository.refreshModelsAsync()
            dataRepository.syncNativeStateAsync()
            viewModelScope.launch {
                dataRepository.denebModels.collect { updateAvailableServices() }
            }
            // The switcher's selected model is workspace-scoped (chatbot role in 챗봇
            // mode, main in 업무), so rebuild it when the workspace flips — not only
            // when the model list changes.
            viewModelScope.launch {
                dataRepository.workspaceWork.collect { updateAvailableServices() }
            }
            viewModelScope.launch {
                dataRepository.denebWorkFeed.collect { feed ->
                    _state.update { it.copy(workFeed = feed.toImmutableList()) }
                }
            }
            viewModelScope.launch {
                dataRepository.workFeedLoaded.collect { loaded ->
                    _state.update { it.copy(workFeedLoaded = loaded) }
                }
            }
        }

        // Keep restoreCurrentConversation off the main thread; see issue #197 (large persisted
        // tool outputs caused ANRs when JSON-decoded synchronously during VM construction).
        viewModelScope.launch(backgroundDispatcher) {
            dataRepository.loadConversations()
            dataRepository.restoreCurrentConversation()
            _state.update { it.copy(isRestoring = false) }
        }

        viewModelScope.launch {
            dataRepository.fallbackStatus.collect { status ->
                _state.update { it.copy(fallbackStatus = status) }
            }
        }
        taskScheduler.start()

        viewModelScope.launch {
            dataRepository.smsDrafts.collect { drafts ->
                _state.update { it.copy(smsDrafts = drafts.toImmutableList()) }
            }
        }

        viewModelScope.launch {
            dataRepository.openHeartbeatRequested
                .filter { it }
                .collect {
                    val heartbeatId = dataRepository.savedConversations.value
                        .firstOrNull { it.type == Conversation.TYPE_HEARTBEAT }?.id
                    if (heartbeatId != null) {
                        loadConversation(heartbeatId)
                        clearUnreadHeartbeat()
                    }
                    dataRepository.consumeOpenHeartbeatRequest()
                }
        }

        // Tapping a proactive-report push opens the 업무 (General) topic, where
        // the report was mirrored — not the heartbeat conversation.
        viewModelScope.launch {
            dataRepository.openWorkTopicRequested
                .filter { it }
                .collect {
                    (dataRepository as? DenebGatewayClient)?.openWorkTopic()
                    // openWorkTopic forces the 업무 workspace — reflect it in the pill.
                    _state.update { it.copy(recallEnabled = true) }
                    dataRepository.consumeOpenWorkTopicRequest()
                }
        }
    }

    // savedConversations summary is recomputed only when the conversation-list
    // reference changes. Streaming emits chatHistory per token, re-running this
    // combine; re-sorting + mapping the whole list every token was wasted work.
    // Safe as plain fields — the combine has a single collector on viewModelScope.
    private var cachedConversationsRef: List<Conversation>? = null
    private var cachedSummaries: ImmutableList<ConversationSummary> = persistentListOf()

    val state = combine(
        _state,
        dataRepository.chatHistory,
        dataRepository.savedConversations,
        dataRepository.currentConversationId,
        // Two unread flags folded into a Pair to stay within combine's 5-arg overload.
        combine(dataRepository.hasUnreadHeartbeat, dataRepository.hasUnreadWorkReport) { hb, wr -> hb to wr },
    ) { state, history, conversations, conversationId, unread ->
        val (hasUnreadHeartbeat, hasUnreadWorkReport) = unread
        if (conversations !== cachedConversationsRef) {
            cachedConversationsRef = conversations
            cachedSummaries = conversations
                .sortedByDescending { it.updatedAt }
                .map {
                    val isHeartbeat = it.type == Conversation.TYPE_HEARTBEAT
                    ConversationSummary(
                        id = it.id,
                        title = if (isHeartbeat) "" else it.title.ifEmpty { getString(Res.string.conversation_untitled) },
                        updatedAt = it.updatedAt,
                        isHeartbeat = isHeartbeat,
                    )
                }
                .toImmutableList()
        }
        state.copy(
            history = history.toImmutableList(),
            supportedFileExtensions = dataRepository.supportedFileExtensions().toImmutableList(),
            savedConversations = cachedSummaries,
            currentConversationId = conversationId,
            hasUnreadHeartbeat = hasUnreadHeartbeat,
            hasUnreadWorkReport = hasUnreadWorkReport,
        )
    }.distinctUntilChanged().stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = _state.value,
    )

    private fun submitUiCallback(event: String, data: Map<String, String>) {
        val message = if (data.isNotEmpty()) {
            val formattedData = data.entries.joinToString(", ") { "${it.key}: ${it.value}" }
            "Responded with: $formattedData"
        } else {
            "Pressed: $event"
        }
        val lastAssistant = dataRepository.chatHistory.value.lastRenderedAssistant()
        val submission = lastAssistant?.let {
            UiSubmission(sourceContent = it.content, values = data, pressedEvent = event)
        }
        askInternal(message, submission)
    }

    private fun ask(question: String?) {
        // The typed-send path: on failure restore the text so it can be edited and
        // resent rather than retyped. retry() passes null, so it carries no restore.
        askInternal(question, null, restoreText = question)
    }

    private fun askInternal(question: String?, uiSubmission: UiSubmission?, restoreText: String? = null) {
        // Prevent concurrent requests
        if (_state.value.isLoading) return

        // Capture files before launching coroutine to avoid race with files being cleared
        val files = _state.value.files

        currentJob = viewModelScope.launch(backgroundDispatcher) {
            _state.update {
                it.copy(
                    isLoading = true,
                    error = null,
                    failedInput = null,
                    files = persistentListOf(),
                )
            }
            try {
                dataRepository.ask(question, files, uiSubmission)

                _state.update {
                    it.copy(isLoading = false)
                }
            } catch (exception: Exception) {
                // CancellationException must be re-thrown to properly propagate coroutine cancellation
                if (exception is CancellationException) throw exception

                _state.update {
                    it.copy(
                        error = exception.toUiError(),
                        isLoading = false,
                        failedInput = restoreText,
                    )
                }
            }
        }
    }

    private fun clearHistory() {
        dataRepository.clearHistory()
        _state.update {
            it.copy(error = null)
        }
    }

    private fun setIsSpeaking(isSpeaking: Boolean, contentId: String) {
        _state.update {
            it.copy(
                isSpeaking = isSpeaking,
                isSpeakingContentId = if (isSpeaking) {
                    contentId
                } else {
                    it.isSpeakingContentId
                },
            )
        }
    }

    private fun addFile(file: PlatformFile) {
        val ext = file.extension.lowercase()
        val supported = dataRepository.supportedFileExtensions()
        if (ext.isEmpty() || ext !in supported) {
            _state.update {
                it.copy(snackbarMessage = Res.string.error_unsupported_file_type)
            }
            return
        }
        _state.update {
            it.copy(files = (it.files + file).toImmutableList())
        }
    }

    private fun removeFile(file: PlatformFile) {
        _state.update {
            it.copy(files = it.files.filterNot { f -> f == file }.toImmutableList())
        }
    }

    private fun clearSnackbar() {
        _state.update {
            it.copy(snackbarMessage = null)
        }
    }

    private fun retry() {
        ask(null)
    }

    private fun toggleSpeechOutput() {
        _state.update {
            it.copy(
                isSpeechOutputEnabled = !it.isSpeechOutputEnabled,
            )
        }
    }

    // Switches workspace (업무 ↔ 챗봇). For the gateway client this also swaps the
    // active session space + its recent-session list (the two never share a list)
    // and persists the recall setting; otherwise it just flips recall. Persona is
    // unchanged — only the session space + whether recall (and retain) fires.
    private fun toggleRecall() {
        val toWork = !_state.value.recallEnabled
        // Gateway client swaps the active session space (업무 ↔ 챗봇) and persists
        // the recall setting; the UI pill reflects it immediately either way.
        (dataRepository as? DenebGatewayClient)?.switchWorkspace(toWork)
        _state.update { it.copy(recallEnabled = toWork) }
    }

    private fun cancel() {
        currentJob?.cancel()
        currentJob = null
        // The partial answer streamed so far stays in the transcript; tag its id so
        // the UI can mark it 중단됨 (otherwise a half-answer looks complete). Cancelling
        // mid-"thinking" leaves no rendered answer, so there's simply nothing to tag.
        val stoppedId = dataRepository.chatHistory.value.lastRenderedAssistant()?.id
        _state.update {
            it.copy(isLoading = false, stoppedMessageId = stoppedId)
        }
    }

    private fun selectService(instanceId: String) {
        // The chat-input switcher lists gateway models; selecting one swaps the
        // active role-model instance on the gateway client.
        (dataRepository as? DenebGatewayClient)?.selectDenebModelInstance(instanceId)
    }

    private fun updateAvailableServices() {
        val entries = (dataRepository as? DenebGatewayClient)
            ?.denebServiceEntries()
            ?.toImmutableList()
            ?: persistentListOf()
        _state.update { it.copy(availableServices = entries, warning = null) }
    }

    // Regenerate = re-ask the last user turn. Drop the last user+assistant pair,
    // then re-send the same user text through the normal ask() path so it streams
    // and shows the loading/cursor UI.
    //
    // The previous `dataRepository.regenerate(); ask(null)` did nothing in
    // gateway mode: regenerate() wasn't overridden by the gateway client (it
    // truncated a different chatHistory instance), and ask(null) sends empty text
    // which the gateway client drops at its `sendText.isEmpty()` guard. Re-asking
    // the captured last-user text fixes the button.
    private fun regenerate() {
        if (_state.value.isLoading) return
        val lastUser = dataRepository.chatHistory.value
            .lastOrNull { it.role == History.Role.USER }
            ?.content
            ?.takeIf { it.isNotBlank() }
            ?: return
        dataRepository.popLastExchange()
        // Re-ask via askInternal (not ask) with no restore text: regenerate is a
        // button, so a failure shouldn't dump the re-asked text into the input box.
        askInternal(lastUser, null, restoreText = null)
    }

    private fun loadConversation(id: String) {
        currentJob?.cancel()
        currentJob = null
        dataRepository.loadConversation(id)
        _state.update {
            it.copy(error = null, isLoading = false)
        }
    }

    private fun deleteConversation(id: String) {
        commitPendingConversationDeletion()
        _state.update { it.copy(pendingConversationDeletion = id) }
        pendingConversationDeleteJob = viewModelScope.launch(backgroundDispatcher) {
            delay(4.seconds)
            dataRepository.deleteConversation(id)
            _state.update { it.copy(pendingConversationDeletion = null) }
        }
    }

    private fun undoDeleteConversation() {
        pendingConversationDeleteJob?.cancel()
        pendingConversationDeleteJob = null
        _state.update { it.copy(pendingConversationDeletion = null) }
    }

    private fun commitPendingConversationDeletion() {
        pendingConversationDeleteJob?.cancel()
        pendingConversationDeleteJob = null
        val pendingId = _state.value.pendingConversationDeletion ?: return
        _state.update { it.copy(pendingConversationDeletion = null) }
        viewModelScope.launch(backgroundDispatcher) {
            dataRepository.deleteConversation(pendingId)
        }
    }

    override fun onCleared() {
        commitPendingConversationDeletion()
        // The scheduler is a process-lifetime singleton (it drives the Android
        // foreground service + gateway event subscriptions), so it deliberately
        // outlives this ViewModel — nothing to tear down here.
        super.onCleared()
    }

    private fun clearUnreadHeartbeat() {
        dataRepository.clearUnreadHeartbeat()
    }

    private fun clearUnreadWorkReport() {
        dataRepository.clearUnreadWorkReport()
    }

    // In-app work-report banner tap: open the 업무 (client:main) home where the
    // proactive report was mirrored, and clear the unread badge.
    private fun openWorkReport() {
        (dataRepository as? DenebGatewayClient)?.openWorkTopic()
        _state.update { it.copy(recallEnabled = true) } // proactive reports = 업무 workspace
        dataRepository.clearUnreadWorkReport()
    }

    private fun openWorkFeedItem(id: String) {
        // Work-feed items live in the 업무 workspace; reflect that in the pill.
        _state.update { it.copy(recallEnabled = true) }
        viewModelScope.launch(backgroundDispatcher) {
            val gateway = dataRepository as? DenebGatewayClient
            val item = _state.value.workFeed.firstOrNull { it.id == id }
            // A proactive report is already mirrored into client:main as a
            // collapsed card — jump there (expanded) instead of spawning a
            // side-conversation whose agent turn re-analyzes what's written.
            // Captures keep the #2110 side-conversation path below.
            if (gateway != null && item != null && item.isProactiveReport) {
                val messageId = gateway.openWorkTopicAtItem(item)
                dataRepository.clearUnreadWorkReport()
                if (messageId != null) {
                    _state.update { it.copy(pendingScrollToMessageId = messageId) }
                }
                return@launch
            }
            val prompt = gateway?.openWorkFeedItem(id)
            dataRepository.clearUnreadWorkReport()
            if (!prompt.isNullOrBlank()) {
                askInternal(prompt, null)
            }
        }
    }

    // The chat list calls this once it has scrolled to the pending target, so a
    // later transcript reload doesn't re-yank the viewport back to the card.
    private fun consumePendingScroll() {
        _state.update { it.copy(pendingScrollToMessageId = null) }
    }

    private fun runWorkFeedAction(itemId: String, actionId: String) {
        viewModelScope.launch(backgroundDispatcher) {
            // The feed quick actions are terminal (보관=ack, 휴지통=trash): they just
            // settle/remove the card, so don't adopt the item's session — a quick
            // action from the feed shouldn't yank the chat over to client:main.
            val prompt = (dataRepository as? DenebGatewayClient)
                ?.runWorkFeedAction(itemId, actionId, adoptSession = false)
            if (!prompt.isNullOrBlank()) {
                askInternal(prompt, null)
            }
        }
    }

    private fun sendSmsDraft(draftId: String) {
        viewModelScope.launch(backgroundDispatcher) {
            dataRepository.sendSmsDraft(draftId)
        }
    }

    private fun discardSmsDraft(draftId: String) {
        viewModelScope.launch(backgroundDispatcher) {
            dataRepository.discardSmsDraft(draftId)
        }
    }

    private fun startNewChat() {
        currentJob?.cancel()
        currentJob = null
        dataRepository.startNewChat()
        _state.update {
            it.copy(error = null, isLoading = false)
        }
    }

    private fun resubmit(messageId: String, event: String, data: Map<String, String>) {
        if (_state.value.isLoading) return
        dataRepository.truncateFrom(messageId)
        submitUiCallback(event, data)
    }

    fun refreshSettings() {
        updateAvailableServices()
        viewModelScope.launch(backgroundDispatcher) {
            dataRepository.restoreCurrentConversation()
        }
    }
}
