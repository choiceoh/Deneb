package ai.deneb.ui.chat

import ai.deneb.data.Conversation
import ai.deneb.data.DataRepository
import ai.deneb.data.SynchronousLock
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
import kotlinx.coroutines.CoroutineStart
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
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
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

private enum class FollowUpPhase {
    STEER_WAITING,
    STEER_IN_FLIGHT,
    QUEUED,
    CLAIMED,
}

private enum class SteerCompletion {
    ACCEPTED,
    QUEUED,
    STALE,
}

private data class TrackedFollowUp(
    val id: Long,
    val pending: PendingQuestion,
    val generation: Long,
    val conversationId: String?,
    val phase: FollowUpPhase,
)

private data class CancelledSteerEntry(
    val steerId: Long?,
    val text: String,
    val restore: Boolean?,
)

private data class CancelledSteerBatch(
    val entries: List<CancelledSteerEntry>,
)

private data class CancelledBatchFlush(
    val ready: Boolean,
    val text: String? = null,
)

private sealed interface AskAdmission {
    data class Direct(val job: Job) : AskAdmission
    data class Steer(val tracked: TrackedFollowUp) : AskAdmission
    data object None : AskAdmission
}

/**
 * Applies [transform] only while [condition] still holds at the compare-and-set.
 *
 * The transform may race another state transition, so retry from the new snapshot
 * instead of overwriting it with a value derived from stale state.
 */
internal fun <T> MutableStateFlow<T>.compareAndUpdateIf(
    condition: (T) -> Boolean,
    transform: (T) -> T,
): Boolean {
    while (true) {
        val current = value
        if (!condition(current)) return false
        if (compareAndSet(current, transform(current))) return true
    }
}

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
        pinConversation = ::pinConversation,
        resetConversationModel = ::resetConversationModel,
        searchConversations = ::searchConversations,
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
    private val steerMutex = Mutex()
    private val steerStateLock = SynchronousLock()
    private var steerGeneration = 0L
    private var nextFollowUpId = 0L
    private var awaitingSuccessfulHandoff = false
    private val trackedFollowUps = mutableListOf<TrackedFollowUp>()
    private val cancelledSteerBatches = mutableMapOf<Long, CancelledSteerBatch>()
    internal var beforeSteerLaunch: (() -> Unit)? = null
    internal var beforeQueuedTurnStart: ((ChatUiState) -> Unit)? = null
    internal var afterSuccessfulHandoffClaim: (() -> Unit)? = null
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
                .sortedWith(compareByDescending<Conversation> { it.pinned }.thenByDescending { it.updatedAt })
                .map {
                    val isHeartbeat = it.type == Conversation.TYPE_HEARTBEAT
                    ConversationSummary(
                        id = it.id,
                        title = if (isHeartbeat) "" else it.title.ifEmpty { getString(Res.string.conversation_untitled) },
                        updatedAt = it.updatedAt,
                        isHeartbeat = isHeartbeat,
                        model = it.model,
                        pinned = it.pinned,
                        snippet = it.snippet,
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
    ) {
        val admission = steerStateLock.withLock {
            if (_state.value.isLoading) {
                // A send while a reply is still streaming: text-only typed
                // follow-ups try mid-turn steer first. Files and programmatic
                // prompts occupy the same ordered after-turn ledger.
                if (question == null || uiSubmission != null) return@withLock AskAdmission.None
                val userTyped = restoreText != null
                val staged = if (userTyped) _state.value.files else persistentListOf()
                val canSteer = userTyped && staged.isEmpty()
                if (canSteer) {
                    AskAdmission.Steer(trackSteerLocked(question))
                } else {
                    trackQueuedFollowUpLocked(question, userTyped)
                    AskAdmission.None
                }
            } else {
                // Loading admission, attachment capture, state claim, and lazy Job
                // installation are one transition. A success/cancel boundary can
                // therefore linearize only before or after this direct turn.
                val files = _state.value.files
                _state.update {
                    it.copy(
                        isLoading = true,
                        error = null,
                        failedInput = null,
                        stoppedMessageId = null,
                        stoppedBeforeAnswer = false,
                        files = persistentListOf(),
                    )
                }
                val job = createTurnJob(
                    question = question,
                    files = files,
                    uiSubmission = uiSubmission,
                    restoreText = restoreText,
                    start = CoroutineStart.LAZY,
                ).also { currentJob = it }
                AskAdmission.Direct(job)
            }
        }

        // Never acquire steerMutex or start coroutine bodies under steerStateLock:
        // RPC completion uses steerMutex -> steerStateLock ordering.
        when (admission) {
            is AskAdmission.Direct -> admission.job.start()

            is AskAdmission.Steer -> {
                beforeSteerLaunch?.invoke()
                launchSteer(admission.tracked)
            }

            AskAdmission.None -> Unit
        }
    }

    private fun launchSteer(tracked: TrackedFollowUp) {
        viewModelScope.launch(teardownHandler, start = CoroutineStart.UNDISPATCHED) {
            var changedHead = false
            var queuedFallback = false
            steerMutex.withLock {
                // Once an earlier typed follow-up has fallen back to the after-turn
                // queue, all later submissions remain behind it after FIFO admission.
                if (queueTrackedSteerBehindPending(tracked)) {
                    changedHead = true
                    queuedFallback = true
                    return@withLock
                }
                if (!markTrackedSteerInFlight(tracked)) return@withLock
                val ok = try {
                    withContext(backgroundDispatcher) {
                        dataRepository.steer(tracked.pending.text)
                    }
                } catch (exception: Exception) {
                    false
                }
                when (completeTrackedSteer(tracked, ok)) {
                    SteerCompletion.ACCEPTED -> changedHead = true

                    SteerCompletion.QUEUED -> {
                        changedHead = true
                        queuedFallback = true
                    }

                    SteerCompletion.STALE -> resolveCancelledSteer(tracked, restore = !ok)
                }
            }
            val resumedHandoff = changedHead && resumeSuccessfulHandoffIfNeeded()
            if (queuedFallback && !resumedHandoff) startNextQueuedIfIdle()
        }
    }

    private fun createTurnJob(
        question: String?,
        files: ImmutableList<PlatformFile>,
        uiSubmission: UiSubmission?,
        restoreText: String?,
        claimedFollowUp: TrackedFollowUp? = null,
        start: CoroutineStart = CoroutineStart.DEFAULT,
    ): Job = viewModelScope.launch(backgroundDispatcher + teardownHandler, start = start) {
        if (claimedFollowUp != null && !beginClaimedTurn(claimedFollowUp)) return@launch
        try {
            val delivered = dataRepository.ask(question, files, uiSubmission)

            if (delivered) {
                // All typed follow-ups admitted before this completion already
                // own earlier FIFO mutex slots. Resolve them before choosing the
                // ledger head, so a later attachment can never overtake an
                // earlier in-flight steer.
                val next = steerMutex.withLock { claimNextQueuedAfterSuccess() }
                afterSuccessfulHandoffClaim?.invoke()
                next?.let(::startQueuedTurn)
            } else {
                // The gateway client surfaces turn failures as an ⚠️ bubble and
                // returns normally — treat that as a failed turn all the same:
                // never auto-send the queue after a failure. Unlike the exception
                // path below, the sent text is already in the transcript (user
                // bubble + error bubble), so only the queue folds into the input.
                steerStateLock.withLock {
                    val flush = snapshotFollowUpsForRestoreLocked(leadingText = null)
                    _state.update {
                        it.copy(
                            isLoading = false,
                            lastSteerNote = null,
                            failedInput = if (flush.ready) flush.text else null,
                            pendingQuestions = persistentListOf(),
                        )
                    }
                }
            }
        } catch (exception: Exception) {
            // CancellationException must be re-thrown to properly propagate coroutine cancellation
            if (exception is CancellationException) throw exception

            steerStateLock.withLock {
                val flush = snapshotFollowUpsForRestoreLocked(leadingText = restoreText)
                _state.update {
                    // Never auto-send queued messages after a FAILED turn — fold them
                    // back into the input (with the failed text) so the user decides.
                    it.copy(
                        error = exception.toUiError(),
                        isLoading = false,
                        lastSteerNote = null,
                        failedInput = if (flush.ready) flush.text else null,
                        pendingQuestions = persistentListOf(),
                    )
                }
            }
        }
    }

    private fun beginClaimedTurn(claimed: TrackedFollowUp): Boolean = steerStateLock.withLock {
        if (steerGeneration != claimed.generation || dataRepository.currentConversationId.value != claimed.conversationId) {
            return@withLock false
        }
        val index = trackedFollowUps.indexOfFirst { it.id == claimed.id && it.phase == FollowUpPhase.CLAIMED }
        if (index < 0) return@withLock false
        trackedFollowUps.removeAt(index)
        true
    }

    private fun startQueuedTurn(job: Job) {
        // Test hook for the exact dequeue/launch cancellation boundary. The Job is
        // already installed as currentJob and its ledger slot remains CLAIMED, so
        // cancel() can both stop it and restore its text before start() is attempted.
        beforeQueuedTurnStart?.invoke(_state.value)
        job.start()
    }

    private fun trackSteerLocked(question: String): TrackedFollowUp {
        val tracked = TrackedFollowUp(
            id = nextFollowUpId++,
            pending = PendingQuestion(text = question, restoreToInput = true),
            generation = steerGeneration,
            conversationId = dataRepository.currentConversationId.value,
            phase = FollowUpPhase.STEER_WAITING,
        )
        trackedFollowUps += tracked
        return tracked
    }

    private fun trackQueuedFollowUpLocked(question: String, restoreToInput: Boolean) {
        val files = if (restoreToInput) _state.value.files else persistentListOf()
        trackedFollowUps += TrackedFollowUp(
            id = nextFollowUpId++,
            pending = PendingQuestion(
                text = question,
                restoreToInput = restoreToInput,
                files = files,
            ),
            generation = steerGeneration,
            conversationId = dataRepository.currentConversationId.value,
            phase = FollowUpPhase.QUEUED,
        )
        _state.update {
            it.copy(
                pendingQuestions = queuedPendingLocked(),
                files = if (restoreToInput) persistentListOf() else it.files,
            )
        }
    }

    private fun queueTrackedSteerBehindPending(tracked: TrackedFollowUp): Boolean = steerStateLock.withLock {
        if (steerGeneration != tracked.generation || dataRepository.currentConversationId.value != tracked.conversationId) {
            return@withLock true
        }
        val index = trackedFollowUps.indexOfFirst { it.id == tracked.id }
        if (index < 0) return@withLock true
        if (trackedFollowUps.none { it.phase == FollowUpPhase.QUEUED }) return@withLock false
        trackedFollowUps[index] = trackedFollowUps[index].copy(phase = FollowUpPhase.QUEUED)
        _state.update { it.copy(pendingQuestions = queuedPendingLocked()) }
        true
    }

    private fun markTrackedSteerInFlight(tracked: TrackedFollowUp): Boolean = steerStateLock.withLock {
        if (steerGeneration != tracked.generation || dataRepository.currentConversationId.value != tracked.conversationId) {
            return@withLock false
        }
        val index = trackedFollowUps.indexOfFirst { it.id == tracked.id }
        if (index < 0) return@withLock false
        trackedFollowUps[index] = trackedFollowUps[index].copy(phase = FollowUpPhase.STEER_IN_FLIGHT)
        true
    }

    private fun completeTrackedSteer(tracked: TrackedFollowUp, accepted: Boolean): SteerCompletion = steerStateLock.withLock {
        if (steerGeneration != tracked.generation || dataRepository.currentConversationId.value != tracked.conversationId) {
            return@withLock SteerCompletion.STALE
        }
        val index = trackedFollowUps.indexOfFirst { it.id == tracked.id }
        if (index < 0) return@withLock SteerCompletion.STALE
        if (accepted) {
            trackedFollowUps.removeAt(index)
            // Removal and the visible acknowledgement share the cancel lock: a
            // stop can win before both or after both, never between them.
            _state.update { it.copy(lastSteerNote = tracked.pending.text) }
            SteerCompletion.ACCEPTED
        } else {
            // Keep the monotonic slot and change only its phase. This is the
            // tracked -> pending commit, so cancel cannot observe a gap.
            trackedFollowUps[index] = trackedFollowUps[index].copy(phase = FollowUpPhase.QUEUED)
            _state.update { it.copy(pendingQuestions = queuedPendingLocked()) }
            SteerCompletion.QUEUED
        }
    }

    private fun resolveCancelledSteer(tracked: TrackedFollowUp, restore: Boolean) {
        steerStateLock.withLock {
            val batch = cancelledSteerBatches[tracked.generation] ?: return@withLock
            val index = batch.entries.indexOfFirst { it.steerId == tracked.id }
            if (index < 0 || batch.entries[index].restore != null) return@withLock
            val entries = batch.entries.toMutableList().apply {
                this[index] = this[index].copy(restore = restore)
            }
            cancelledSteerBatches[tracked.generation] = batch.copy(entries = entries)
            val flush = flushCancelledBatchesLocked()
            if (flush.ready) _state.update { it.copy(failedInput = flush.text) }
        }
    }

    private fun cancelledRestoreText(entries: List<CancelledSteerEntry>): String? = entries
        .filter { it.restore == true }
        .joinToString("\n\n") { it.text }
        .ifBlank { null }

    private fun flushCancelledBatchesLocked(): CancelledBatchFlush {
        if (cancelledSteerBatches.isEmpty() || cancelledSteerBatches.values.any { batch -> batch.entries.any { it.restore == null } }) {
            return CancelledBatchFlush(ready = false)
        }
        val text = cancelledSteerBatches.toSortedMap().values
            .flatMap { it.entries }
            .let(::cancelledRestoreText)
        cancelledSteerBatches.clear()
        return CancelledBatchFlush(ready = true, text = text)
    }

    private fun snapshotFollowUpsForRestoreLocked(leadingText: String?): CancelledBatchFlush {
        val snapshotGeneration = steerGeneration
        steerGeneration++
        awaitingSuccessfulHandoff = false
        val entries = buildList {
            if (leadingText != null) {
                add(CancelledSteerEntry(steerId = null, text = leadingText, restore = true))
            }
            trackedFollowUps.sortedBy { it.id }.forEach {
                add(
                    CancelledSteerEntry(
                        steerId = it.id,
                        text = it.pending.text,
                        restore = when (it.phase) {
                            FollowUpPhase.STEER_WAITING -> true

                            FollowUpPhase.STEER_IN_FLIGHT -> null

                            FollowUpPhase.QUEUED,
                            FollowUpPhase.CLAIMED,
                            -> it.pending.restoreToInput
                        },
                    ),
                )
            }
        }
        trackedFollowUps.clear()
        if (entries.isNotEmpty()) {
            cancelledSteerBatches[snapshotGeneration] = CancelledSteerBatch(entries)
        }
        return flushCancelledBatchesLocked()
    }

    private fun queuedPendingLocked() = trackedFollowUps
        .asSequence()
        .filter { it.phase == FollowUpPhase.QUEUED }
        .sortedBy { it.id }
        .map { it.pending }
        .toList()
        .toImmutableList()

    private fun invalidateTrackedSteersForSessionBoundary() {
        steerStateLock.withLock {
            steerGeneration++
            awaitingSuccessfulHandoff = false
            trackedFollowUps.clear()
            cancelledSteerBatches.clear()
        }
    }

    private fun claimNextQueuedAfterSuccess(): Job? = steerStateLock.withLock {
        awaitingSuccessfulHandoff = true
        advanceSuccessfulHandoffLocked()
    }

    private fun resumeSuccessfulHandoffIfNeeded(): Boolean {
        var job: Job? = null
        val active = steerStateLock.withLock {
            if (!awaitingSuccessfulHandoff) return@withLock false
            job = advanceSuccessfulHandoffLocked()
            true
        }
        job?.let(::startQueuedTurn)
        return active
    }

    private fun advanceSuccessfulHandoffLocked(): Job? {
        val headIndex = trackedFollowUps.indices.minByOrNull { trackedFollowUps[it].id }
        if (headIndex == null) {
            awaitingSuccessfulHandoff = false
            _state.update { it.copy(isLoading = false, lastSteerNote = null) }
            return null
        }
        if (trackedFollowUps[headIndex].phase != FollowUpPhase.QUEUED) {
            // Admission reserved a ledger slot but has not yet acquired
            // steerMutex. Keep loading claimed; its resolution resumes this handoff.
            return null
        }
        awaitingSuccessfulHandoff = false
        return installQueuedTurnLocked(headIndex)
    }

    private fun installQueuedTurnLocked(index: Int): Job {
        val claimed = trackedFollowUps[index].copy(phase = FollowUpPhase.CLAIMED)
        trackedFollowUps[index] = claimed
        // Dequeue and claim the next turn before state observers can see idle.
        // A later submit therefore cannot overtake this ingress slot.
        _state.update {
            it.copy(
                isLoading = true,
                error = null,
                failedInput = null,
                lastSteerNote = null,
                stoppedMessageId = null,
                stoppedBeforeAnswer = false,
                pendingQuestions = queuedPendingLocked(),
            )
        }
        clearAnswerVariants()
        return createTurnJob(
            question = claimed.pending.text,
            files = claimed.pending.files,
            uiSubmission = null,
            restoreText = if (claimed.pending.restoreToInput) claimed.pending.text else null,
            claimedFollowUp = claimed,
            start = CoroutineStart.LAZY,
        ).also { currentJob = it }
    }

    private fun startNextQueuedIfIdle() {
        val job = steerStateLock.withLock {
            if (_state.value.isLoading) return@withLock null
            val index = nextQueuedIndexLocked() ?: return@withLock null
            installQueuedTurnLocked(index)
        } ?: return
        startQueuedTurn(job)
    }

    private fun nextQueuedIndexLocked(): Int? {
        val headIndex = trackedFollowUps.indices.minByOrNull { trackedFollowUps[it].id } ?: return null
        return headIndex.takeIf { trackedFollowUps[it].phase == FollowUpPhase.QUEUED }
    }

    private fun cancelPendingQuestions() {
        steerStateLock.withLock {
            trackedFollowUps.removeAll { it.phase == FollowUpPhase.QUEUED }
            _state.update { it.copy(pendingQuestions = queuedPendingLocked()) }
        }
    }

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
        // Tag 중단됨 only on the answer that was actually streaming. Cancelling
        // mid-thinking leaves no new tokens — lastRenderedAssistant() would be the
        // previous complete reply, which must not be marked stopped.
        val history = dataRepository.chatHistory.value
        val beforeAnswer = history.hasUnansweredUserTurn()
        val stoppedId = if (beforeAnswer) null else history.lastRenderedAssistant()?.id
        val jobToCancel = steerStateLock.withLock {
            // Job ownership and the ledger generation move together. Otherwise a
            // success handoff can install B after cancel() already cleared A but
            // before the cancel snapshot, leaving B neither cancelled nor tracked.
            val capturedJob = currentJob
            currentJob = null
            val flush = snapshotFollowUpsForRestoreLocked(leadingText = null)
            _state.update {
                // Snapshot and clear the unified queue in one emission. When any
                // generation still has an unresolved RPC, failedInput is published
                // once after all retained cancel batches can be merged in order.
                it.copy(
                    isLoading = false,
                    lastSteerNote = null,
                    stoppedMessageId = stoppedId,
                    stoppedBeforeAnswer = beforeAnswer,
                    failedInput = if (flush.ready) flush.text else null,
                    pendingQuestions = persistentListOf(),
                )
            }
            capturedJob
        }
        jobToCancel?.cancel()
        // Cancelling the local job only detaches the UI. The gateway runs the turn
        // off the request context on purpose (so a backgrounded phone does not kill
        // it), so without this the model kept generating after the stop and its
        // answer replaced the "중단됨" reply at the next reconcile. Fire-and-forget
        // on its own scope: the job above is already cancelled, and a stop must not
        // wait on the network.
        viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            dataRepository.abortTurn()
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
        invalidateTrackedSteersForSessionBoundary()
        currentJob?.cancel()
        currentJob = null
        clearAnswerVariants() // variants are per-live-answer, never cross sessions
        dataRepository.loadConversation(id)
        _state.update {
            // Queued messages belong to the conversation they were sent in — they
            // must never auto-fire or appear in the destination composer. (The
            // cancel above rethrows CancellationException inside askInternal, so
            // its catch-side queue fold never runs — clear here.) Composer drafts
            // are per-session; failedInput is cleared so the old queue cannot
            // restore into the wrong conversation.
            it.copy(
                error = null,
                isLoading = false,
                lastSteerNote = null,
                failedInput = null,
                pendingQuestions = persistentListOf(),
                sessionSearchHits = persistentListOf(),
            )
        }
    }

    private fun pinConversation(id: String, pinned: Boolean) {
        if (id.isBlank()) return
        viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            dataRepository.pinConversation(id, pinned)
        }
    }

    private fun resetConversationModel(id: String) {
        if (id.isBlank()) return
        viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            dataRepository.resetConversationModel(id)
        }
    }

    private fun searchConversations(query: String) {
        val q = query.trim()
        if (q.isEmpty()) {
            _state.update { it.copy(sessionSearchHits = persistentListOf()) }
            return
        }
        viewModelScope.launch(backgroundDispatcher + teardownHandler) {
            val hits = dataRepository.searchConversations(q).map { conv ->
                ConversationSummary(
                    id = conv.id,
                    title = conv.title.ifEmpty { conv.id },
                    updatedAt = conv.updatedAt,
                    model = conv.model,
                    pinned = conv.pinned,
                    snippet = conv.snippet,
                )
            }.toImmutableList()
            _state.update { it.copy(sessionSearchHits = hits) }
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
        invalidateTrackedSteersForSessionBoundary()
        currentJob?.cancel()
        currentJob = null
        clearAnswerVariants()
        dataRepository.startNewChat()
        _state.update {
            // Same queue hygiene as loadConversation: never carry queued messages
            // into the fresh conversation, and clear any stale failedInput.
            it.copy(
                error = null,
                isLoading = false,
                lastSteerNote = null,
                failedInput = null,
                pendingQuestions = persistentListOf(),
                sessionSearchHits = persistentListOf(),
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
