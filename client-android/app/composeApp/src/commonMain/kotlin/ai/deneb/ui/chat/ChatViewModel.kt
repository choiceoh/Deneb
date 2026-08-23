package ai.deneb.ui.chat

import ai.deneb.data.Conversation
import ai.deneb.data.DataRepository
import ai.deneb.data.TaskScheduler
import ai.deneb.data.UiSubmission
import ai.deneb.data.isStageableExtension
import ai.deneb.data.isWithinAttachmentSize
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.answerWorkFeedItem
import ai.deneb.deneb.denebServiceEntries
import ai.deneb.deneb.markWorkFeedRead
import ai.deneb.deneb.openWorkFeedItem
import ai.deneb.deneb.openWorkTopic
import ai.deneb.deneb.openWorkTopicAtItem
import ai.deneb.deneb.refreshModelsAsync
import ai.deneb.deneb.refreshWorkFeed
import ai.deneb.deneb.rewriteWorkFeedCard
import ai.deneb.deneb.runWorkFeedAction
import ai.deneb.deneb.selectDenebModelInstance
import ai.deneb.deneb.sendWorkFeedFeedback
import ai.deneb.deneb.syncNativeStateAsync
import ai.deneb.getBackgroundDispatcher
import ai.deneb.network.httpTeardownTolerantHandler
import ai.deneb.network.toUiError
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.conversation_untitled
import deneb.composeapp.generated.resources.error_conversation_delete_failed
import deneb.composeapp.generated.resources.error_conversation_rename_failed
import deneb.composeapp.generated.resources.error_file_too_large
import deneb.composeapp.generated.resources.error_unsupported_file_type
import io.github.vinceglb.filekit.PlatformFile
import io.github.vinceglb.filekit.extension
import io.github.vinceglb.filekit.size
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.sample
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.jetbrains.compose.resources.getString
import kotlin.coroutines.CoroutineContext
import kotlin.time.Duration.Companion.seconds

// During streaming chatHistory emits once per token (tens per second). Sampling it to this
// cadence caps the combine's per-token cost — a whole-history list copy plus a
// distinctUntilChanged equality over every message — to ~15x/s. The visible text only reflows
// ~10x/s anyway (STREAM_PARSE_INTERVAL_MS), and Compose already coalesces render to the frame,
// so nothing visible is lost; non-streaming history changes are delayed at most this long
// (imperceptible). Other combine inputs (isLoading, …) stay unsampled, so stream start/end and
// the streaming caret still flip instantly.
private const val STREAM_HISTORY_SAMPLE_MS = 64L

class ChatViewModel(
    private val dataRepository: DataRepository,
    private val taskScheduler: TaskScheduler,
    private val backgroundDispatcher: CoroutineContext = getBackgroundDispatcher(),
) : ViewModel() {

    private val actions = ChatActions(
        ask = ::ask,
        retry = ::dismissError,
        toggleSpeechOutput = ::toggleSpeechOutput,
        clearHistory = ::clearHistory,
        setIsSpeaking = ::setIsSpeaking,
        addFile = ::addFile,
        removeFile = ::removeFile,
        startNewChat = ::startNewChat,
        regenerate = ::regenerate,
        editResendLast = ::editResendLast,
        selectAnswerVariant = ::selectAnswerVariant,
        cancel = ::cancel,
        cancelPendingQuestions = ::cancelPendingQuestions,
        selectService = ::selectService,
        loadConversation = ::loadConversation,
        deleteConversation = ::deleteConversation,
        renameConversation = ::renameConversation,
        clearUnreadHeartbeat = ::clearUnreadHeartbeat,
        clearUnreadWorkReport = ::clearUnreadWorkReport,
        openWorkReport = ::openWorkReport,
        openWorkFeedItem = ::openWorkFeedItem,
        markWorkFeedRead = ::markWorkFeedRead,
        refreshWorkFeedRange = ::refreshWorkFeedRange,
        consumePendingScroll = ::consumePendingScroll,
        runWorkFeedAction = ::runWorkFeedAction,
        answerWorkFeed = ::answerWorkFeed,
        submitWorkFeedFeedback = ::submitWorkFeedFeedback,
        rewriteWorkFeedCard = ::rewriteWorkFeedCard,
        clearSnackbar = ::clearSnackbar,
        clearFeedbackResult = ::clearFeedbackResult,
        undoDeleteConversation = ::undoDeleteConversation,
        submitUiCallback = ::submitUiCallback,
        resubmit = ::resubmit,
        sendSmsDraft = ::sendSmsDraft,
        discardSmsDraft = ::discardSmsDraft,
        refreshConversations = { dataRepository.loadConversations() },
        loadMoreConversations = { dataRepository.loadMoreConversations() },
    )

    // In the context of every job that can be cancel()ed while a gateway request
    // streams (stop button, conversation switch, delete-undo, VM clear): without
    // it the platform-okhttp teardown exception reaches the uncaught handler and
    // kills the app — crash reporter build 614, stop tapped mid-stream. See
    // httpTeardownTolerantHandler.
    private val teardownHandler = httpTeardownTolerantHandler("ChatViewModel")
    private var currentJob: Job? = null
    private var pendingConversationDeleteJob: Job? = null
    private val _state = MutableStateFlow(
        ChatUiState(
            actions = actions,
        ),
    )

    init {
        updateAvailableServices()
        // Deneb: the chat-input model switcher lists gateway models; rebuild it
        // whenever the model registry changes (after a switch or on first load).
        if (dataRepository is DenebGatewayClient) {
            dataRepository.refreshModelsAsync()
            dataRepository.syncNativeStateAsync()
            viewModelScope.launch {
                dataRepository.denebModels.collect { updateAvailableServices() }
            }
            viewModelScope.launch {
                dataRepository.sessionModels.collect { updateAvailableServices() }
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
            dataRepository.currentConversationId.collect { updateAvailableServices() }
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

    @OptIn(FlowPreview::class)
    val state = combine(
        _state,
        // Sampled (see STREAM_HISTORY_SAMPLE_MS): the per-token stream would otherwise re-run
        // this whole combine — including the history list copy + equality below — every token.
        dataRepository.chatHistory.sample(STREAM_HISTORY_SAMPLE_MS),
        dataRepository.savedConversations,
        dataRepository.currentConversationId,
        // Two unread flags + the drawer's has-more flag folded into a Triple to
        // stay within combine's 5-arg overload.
        combine(
            dataRepository.hasUnreadHeartbeat,
            dataRepository.hasUnreadWorkReport,
            dataRepository.hasMoreConversations,
        ) { hb, wr, more -> Triple(hb, wr, more) },
    ) { state, history, conversations, conversationId, flags ->
        val (hasUnreadHeartbeat, hasUnreadWorkReport, hasMoreConversations) = flags
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
            hasMoreConversations = hasMoreConversations,
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
        // A NEW question makes the previous answer's ‹ n/N › stash meaningless.
        if (question != null) clearAnswerVariants()
        // The typed-send path: on failure restore the text so it can be edited and
        // resent rather than retyped. retry() passes null, so it carries no restore.
        askInternal(question, null, restoreText = question)
    }

    private fun askInternal(
        question: String?,
        uiSubmission: UiSubmission?,
        restoreText: String? = null,
        // Attachment snapshot carried by a drained queue entry. null = direct send
        // (consume the files staged in the UI); non-null (possibly empty) = queued
        // send whose attachments were captured at queue time, so it never absorbs
        // files the user staged for a LATER message.
        presetFiles: ImmutableList<PlatformFile>? = null,
    ) {
        // A send while a reply is still streaming: text-only typed follow-ups
        // try mid-turn steer first. If the gateway declines (or the send has
        // files / is programmatic), it QUEUES and fires when this turn completes
        // (see drainPendingQuestion). Retries (question == null) and UI-card
        // submissions belong to the CURRENT turn and must not replay after it.
        if (_state.value.isLoading) {
            if (question != null && uiSubmission == null) {
                val userTyped = restoreText != null
                val staged = if (userTyped) _state.value.files else persistentListOf()
                // Text-only typed follow-up: try mid-turn steer first. Files and
                // programmatic prompts stay on the after-turn queue.
                val canSteer = userTyped && staged.isEmpty() && (presetFiles == null || presetFiles.isEmpty())
                if (canSteer) {
                    viewModelScope.launch(backgroundDispatcher + teardownHandler) {
                        val ok = try {
                            dataRepository.steer(question)
                        } catch (exception: Exception) {
                            if (exception is CancellationException) throw exception
                            false
                        }
                        if (ok) {
                            _state.update { it.copy(lastSteerNote = question) }
                            return@launch
                        }
                        if (_state.value.isLoading) {
                            _state.update {
                                it.copy(
                                    pendingQuestions = (
                                        it.pendingQuestions + PendingQuestion(
                                            text = question,
                                            restoreToInput = true,
                                        )
                                        ).toImmutableList(),
                                )
                            }
                        } else {
                            askInternal(question, null, restoreText = question)
                        }
                    }
                    return
                }
                _state.update {
                    val entry = PendingQuestion(
                        text = question,
                        restoreToInput = userTyped,
                        files = if (userTyped) it.files else persistentListOf(),
                    )
                    it.copy(
                        pendingQuestions = (it.pendingQuestions + entry).toImmutableList(),
                        // The staged files now belong to the queued message.
                        files = if (userTyped) persistentListOf() else it.files,
                    )
                }
            }
            return
        }

        // Capture files before launching coroutine to avoid race with files being cleared
        val files = presetFiles ?: _state.value.files

        currentJob = viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            _state.update {
                it.copy(
                    isLoading = true,
                    error = null,
                    failedInput = null,
                    stoppedMessageId = null,
                    stoppedBeforeAnswer = false,
                    // Only a direct send consumes the staged files; a drained queue
                    // entry carries its own snapshot, so anything staged since then
                    // stays with the user's next draft.
                    files = if (presetFiles == null) persistentListOf() else it.files,
                )
            }
            try {
                val delivered = dataRepository.ask(question, files, uiSubmission)

                if (delivered) {
                    _state.update {
                        it.copy(isLoading = false, lastSteerNote = null)
                    }
                    drainPendingQuestion()
                } else {
                    // The gateway client surfaces turn failures as an ⚠️ bubble and
                    // returns normally — treat that as a failed turn all the same:
                    // never auto-send the queue after a failure. Unlike the exception
                    // path below, the sent text is already in the transcript (user
                    // bubble + error bubble), so only the queue folds into the input.
                    _state.update {
                        it.copy(
                            isLoading = false,
                            lastSteerNote = null,
                            failedInput = foldIntoInput(null, it.pendingQuestions),
                            pendingQuestions = persistentListOf(),
                        )
                    }
                }
            } catch (exception: Exception) {
                // CancellationException must be re-thrown to properly propagate coroutine cancellation
                if (exception is CancellationException) throw exception

                _state.update {
                    // Never auto-send queued messages after a FAILED turn — fold them
                    // back into the input (with the failed text) so the user decides.
                    it.copy(
                        error = exception.toUiError(),
                        isLoading = false,
                        lastSteerNote = null,
                        failedInput = foldIntoInput(restoreText, it.pendingQuestions),
                        pendingQuestions = persistentListOf(),
                    )
                }
            }
        }
    }

    // Fires the next queued message once the running turn has completed
    // successfully. Success-path only: an errored/stopped turn folds the queue
    // back into the input instead (the user may want to rephrase).
    private fun drainPendingQuestion() {
        val next = _state.value.pendingQuestions.firstOrNull() ?: return
        clearAnswerVariants() // a queued NEW question, same as ask()
        _state.update { it.copy(pendingQuestions = it.pendingQuestions.drop(1).toImmutableList()) }
        // A drained programmatic prompt keeps its no-restore semantics: if THIS
        // turn fails, its text must not land in the input box either.
        askInternal(
            next.text,
            null,
            restoreText = if (next.restoreToInput) next.text else null,
            presetFiles = next.files,
        )
    }

    private fun cancelPendingQuestions() {
        _state.update { it.copy(pendingQuestions = persistentListOf()) }
    }

    // Joins the failed text and the user-typed queued messages into one
    // input-restore blob (blank → null so the restore effect stays quiet).
    // Programmatic queue entries are dropped — their text must never surface
    // verbatim in the input box.
    private fun foldIntoInput(restoreText: String?, queued: List<PendingQuestion>): String? = (listOfNotNull(restoreText) + queued.filter { it.restoreToInput }.map { it.text })
        .joinToString("\n\n").ifBlank { null }

    private fun clearHistory() {
        clearAnswerVariants()
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
        if (!isStageableExtension(file.extension, dataRepository.supportedFileExtensions())) {
            _state.update {
                it.copy(snackbarMessage = Res.string.error_unsupported_file_type)
            }
            return
        }
        // Skip the SAME file picked twice. Matched by identity, not by name: two
        // different files can share a name (Download/report.pdf and the one in
        // Documents), and dropping the second silently lost an attachment the user
        // had explicitly picked.
        if (_state.value.files.any { it == file }) return
        viewModelScope.launch {
            // Cap non-image uploads (images are downsampled before send) so a huge pick
            // can't OOM the device or bloat the turn payload — checked via size(), which
            // does not read the whole file into memory.
            val size = runCatching { file.size() }.getOrNull() ?: 0L
            if (!isWithinAttachmentSize(file.extension, size)) {
                _state.update { it.copy(snackbarMessage = Res.string.error_file_too_large) }
                return@launch
            }
            _state.update {
                if (it.files.any { staged -> staged == file }) {
                    it
                } else {
                    it.copy(files = (it.files + file).toImmutableList())
                }
            }
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

    // The error card's action. It does NOT resend: by the time the card is on
    // screen the failed text is already back in the composer (failedInput ->
    // ChatModeScreen's restore effect), so resending would put the same message in
    // twice. The user retries by pressing send; this just clears the banner.
    // (It used to call ask(null), which askGateway drops on the empty-text guard —
    // the button looked like a retry and did nothing at all.)
    private fun dismissError() {
        _state.update { it.copy(error = null) }
    }

    private fun toggleSpeechOutput() {
        _state.update {
            it.copy(
                isSpeechOutputEnabled = !it.isSpeechOutputEnabled,
            )
        }
    }

    private fun cancel() {
        currentJob?.cancel()
        currentJob = null
        // Tag 중단됨 only on the answer that was actually streaming. Cancelling
        // mid-thinking leaves no new tokens — lastRenderedAssistant() would be the
        // previous complete reply, which must not be marked stopped.
        val history = dataRepository.chatHistory.value
        val beforeAnswer = history.hasUnansweredUserTurn()
        val stoppedId = if (beforeAnswer) null else history.lastRenderedAssistant()?.id
        _state.update {
            // A stop also cancels the auto-send of queued messages (the user pulled
            // the brake) — fold them back into the input instead of firing them.
            it.copy(
                isLoading = false,
                lastSteerNote = null,
                stoppedMessageId = stoppedId,
                stoppedBeforeAnswer = beforeAnswer,
                failedInput = foldIntoInput(null, it.pendingQuestions),
                pendingQuestions = persistentListOf(),
            )
        }
    }

    private fun selectService(instanceId: String) {
        // The chat-input switcher lists gateway models; selecting one binds
        // that model to the current conversation only.
        (dataRepository as? DenebGatewayClient)?.selectDenebModelInstance(instanceId)
    }

    private fun updateAvailableServices() {
        val sessionKey = dataRepository.currentConversationId.value
        val entries = (dataRepository as? DenebGatewayClient)
            ?.denebServiceEntries(sessionKey)
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
        // Stash the answer being replaced so ‹ n/N › can navigate back to it —
        // the pop below only rewinds the VISIBLE history, so without a stash the
        // previous version is simply gone.
        stashLastAnswerVariant()
        dataRepository.popLastExchange()
        // Re-ask via askInternal (not ask) with no restore text: regenerate is a
        // button, so a failure shouldn't dump the re-asked text into the input box.
        askInternal(lastUser, null, restoreText = null)
    }

    // 마지막 사용자 메시지를 고친 텍스트로 다시 보낸다 — regenerate와 동일한 되감기에
    // 텍스트만 다르다. 질문이 바뀌므로 이전 답변 변형은 무의미해져 함께 비운다.
    private fun editResendLast(newText: String) {
        val text = newText.trim()
        if (text.isEmpty() || _state.value.isLoading) return
        val hasUser = dataRepository.chatHistory.value.any { it.role == History.Role.USER }
        if (!hasUser) return
        clearAnswerVariants()
        dataRepository.popLastExchange()
        // 실패 시 고친 텍스트가 입력창으로 복원되도록 restoreText를 싣는다 (ask와 동일).
        askInternal(text, null, restoreText = text)
    }

    private fun selectAnswerVariant(index: Int) {
        _state.update {
            val clamped = index.coerceIn(0, it.lastAnswerVariants.size)
            it.copy(lastAnswerVariantIndex = clamped)
        }
    }

    private fun stashLastAnswerVariant() {
        val previous = dataRepository.chatHistory.value.lastRenderedAssistant() ?: return
        _state.update {
            val variants = (it.lastAnswerVariants + previous).toImmutableList()
            // Index parks on the new LIVE answer (== variants.size) so regenerate
            // always shows the fresh reply; ‹ steps back into the stash.
            it.copy(lastAnswerVariants = variants, lastAnswerVariantIndex = variants.size)
        }
    }

    private fun clearAnswerVariants() {
        _state.update {
            if (it.lastAnswerVariants.isEmpty() && it.lastAnswerVariantIndex == 0) {
                it
            } else {
                it.copy(lastAnswerVariants = persistentListOf(), lastAnswerVariantIndex = 0)
            }
        }
    }

    private fun loadConversation(id: String) {
        currentJob?.cancel()
        currentJob = null
        clearAnswerVariants() // variants are per-live-answer, never cross sessions
        dataRepository.loadConversation(id)
        _state.update {
            // Queued messages belong to the conversation they were sent in — they
            // must never auto-fire into the one we're switching to. (The cancel
            // above rethrows CancellationException inside askInternal, so its
            // catch-side queue fold never runs — clear here.) User-typed entries
            // are restored into the input box via failedInput; programmatic card
            // prompts are dropped (their card side effects already ran). An empty
            // queue folds to null, which also clears any stale failedInput so it
            // can't restore into the wrong conversation later.
            it.copy(
                error = null,
                isLoading = false,
                lastSteerNote = null,
                failedInput = foldIntoInput(null, it.pendingQuestions),
                pendingQuestions = persistentListOf(),
            )
        }
    }

    private fun renameConversation(id: String, label: String) {
        val trimmed = label.trim()
        if (id.isBlank() || trimmed.isEmpty()) return
        viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            // A rename that did not reach the gateway used to return silently, so
            // the label simply stayed as it was with no hint why.
            if (!dataRepository.renameConversation(id, trimmed)) {
                _state.update { it.copy(snackbarMessage = Res.string.error_conversation_rename_failed) }
            }
        }
    }

    private fun deleteConversation(id: String) {
        commitPendingConversationDeletion()
        _state.update { it.copy(pendingConversationDeletion = id) }
        pendingConversationDeleteJob = viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            delay(4.seconds)
            // Deleting the conversation you are reading has to move you out of it.
            // Staying put left the composer bound to a session that no longer
            // exists, so the next message silently recreated it as a ghost.
            val wasOpen = dataRepository.currentConversationId.value == id
            val deleted = dataRepository.deleteConversation(id)
            _state.update { it.copy(pendingConversationDeletion = null) }
            if (!deleted) {
                _state.update { it.copy(snackbarMessage = Res.string.error_conversation_delete_failed) }
                return@launch
            }
            if (wasOpen) startNewChat()
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
        // onCleared() lands here and then cancels viewModelScope, which can catch
        // this delete RPC mid-flight — teardown-tolerant like the other jobs.
        viewModelScope.launch(backgroundDispatcher + teardownHandler) {
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
        dataRepository.clearUnreadWorkReport()
    }

    private fun openWorkFeedItem(id: String) {
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

    // Opening a card reads it: tell the gateway so the read state is durable and shows
    // read on the desktop too. The local seen-set drives the immediate in-feed dim.
    private fun markWorkFeedRead(id: String) {
        if (id.isBlank()) return
        viewModelScope.launch(backgroundDispatcher) {
            (dataRepository as? DenebGatewayClient)?.markWorkFeedRead(id)
        }
    }

    private suspend fun refreshWorkFeedRange(sinceMs: Long, beforeMs: Long): Boolean {
        if (sinceMs <= 0L || beforeMs <= sinceMs) return false
        return withContext(backgroundDispatcher) {
            (dataRepository as? DenebGatewayClient)
                ?.refreshWorkFeed(sinceMs = sinceMs, beforeMs = beforeMs, merge = true)
                ?: false
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

    // Answer a question card inline (Toss-style). A choice chip (actionId set) runs
    // that action — settles the card, records deal-team answers server-side, and for
    // ActionAnswer returns the choice as a prompt. A free-text reply (actionId null)
    // acks the card and routes the typed answer to the card's asking session. Either
    // way the returned prompt is delivered as a turn so the agent reacts to it.
    private fun answerWorkFeed(item: WorkFeedItem, answer: String, actionId: String?, comment: String?) {
        viewModelScope.launch(backgroundDispatcher) {
            val gw = dataRepository as? DenebGatewayClient ?: return@launch
            val prompt = if (actionId != null) {
                gw.runWorkFeedAction(item.id, actionId, comment) // adoptSession=true → routes to the asking session
            } else {
                if (answer.isBlank()) return@launch
                gw.answerWorkFeedItem(item.id, answer)
            }
            if (!prompt.isNullOrBlank()) askInternal(prompt, null)
        }
    }

    // Fire-and-forget a card correction from the feed long-press sheet. The gateway
    // annotates the card and runs one (ephemeral) agent turn to fix the durable wiki
    // knowledge; the returned annotated item is upserted into the feed by the client.
    // Runs on viewModelScope so it survives the sheet closing.
    private fun submitWorkFeedFeedback(itemId: String, feedback: String) {
        if (itemId.isBlank() || feedback.isBlank()) return
        viewModelScope.launch(backgroundDispatcher) {
            val report = (dataRepository as? DenebGatewayClient)?.sendWorkFeedFeedback(itemId, feedback)
            if (!report.isNullOrBlank()) {
                _state.update { it.copy(feedbackResultText = report) }
            }
        }
    }

    // Clears the feed-card feedback report after the snackbar has shown it.
    private fun clearFeedbackResult() {
        _state.update { it.copy(feedbackResultText = null) }
    }

    // Fire-and-forget a card rewrite from the feed long-press sheet. The gateway runs
    // one (ephemeral) agent turn that regenerates the analysis and replaces the card
    // body; the returned card is upserted into the feed. Runs on viewModelScope so it
    // survives the sheet closing.
    private fun rewriteWorkFeedCard(itemId: String) {
        if (itemId.isBlank()) return
        viewModelScope.launch(backgroundDispatcher) {
            (dataRepository as? DenebGatewayClient)?.rewriteWorkFeedCard(itemId)
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
        clearAnswerVariants()
        dataRepository.startNewChat()
        _state.update {
            // Same queue hygiene as loadConversation: never carry queued messages
            // into the fresh conversation — restore user-typed ones to the input,
            // drop programmatic ones, and clear any stale failedInput.
            it.copy(
                error = null,
                isLoading = false,
                lastSteerNote = null,
                failedInput = foldIntoInput(null, it.pendingQuestions),
                pendingQuestions = persistentListOf(),
            )
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
