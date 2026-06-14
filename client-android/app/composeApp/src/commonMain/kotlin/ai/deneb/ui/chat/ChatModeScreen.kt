@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat

import ai.deneb.Platform
import ai.deneb.currentPlatform
import ai.deneb.getBackgroundDispatcher
import ai.deneb.onDragAndDropEventDropped
import ai.deneb.ui.chat.composables.BotMessage
import ai.deneb.ui.chat.composables.DenebDrawerSheet
import ai.deneb.ui.chat.composables.DenebSessionDrawerSheet
import ai.deneb.ui.chat.composables.EmptyState
import ai.deneb.ui.chat.composables.ErrorMessage
import ai.deneb.ui.chat.composables.HeartbeatBanner
import ai.deneb.ui.chat.composables.PendingSmsBanners
import ai.deneb.ui.chat.composables.QuestionInput
import ai.deneb.ui.chat.composables.TopBar
import ai.deneb.ui.chat.composables.UserMessage
import ai.deneb.ui.chat.composables.WaitingResponseRow
import ai.deneb.ui.chat.composables.WorkFeedPanel
import ai.deneb.ui.chat.composables.WorkReportBanner
import ai.deneb.ui.chat.composables.uiErrorText
import ai.deneb.ui.components.VerticalScrollbarForList
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebContentWidthModifier
import ai.deneb.ui.denebPopEnter
import ai.deneb.ui.denebPopExit
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.handCursor
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.draganddrop.dragAndDropTarget
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.layout.LazyLayoutCacheWindow
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.SmallFloatingActionButton
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment.Companion.BottomCenter
import androidx.compose.ui.Alignment.Companion.CenterEnd
import androidx.compose.ui.Alignment.Companion.CenterHorizontally
import androidx.compose.ui.Alignment.Companion.TopCenter
import androidx.compose.ui.Modifier
import androidx.compose.ui.draganddrop.DragAndDropEvent
import androidx.compose.ui.draganddrop.DragAndDropTarget
import androidx.compose.ui.draw.blur
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.fallback_answered_by
import deneb.composeapp.generated.resources.fallback_service_failed
import deneb.composeapp.generated.resources.fallback_trying_next
import deneb.composeapp.generated.resources.scroll_to_bottom_content_description
import deneb.composeapp.generated.resources.tool_footprint
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.errors.TextToSpeechSynthesisInterruptedError
import org.jetbrains.compose.resources.getString
import org.jetbrains.compose.resources.stringResource
import kotlin.time.TimeSource

/**
 * Regular chat mode of the chat surface: the message list, input chrome, banners,
 * and the executing-tools indicator. Split out of ChatScreen.kt so the mode can
 * grow without re-bloating the entry file.
 */
@Composable
internal fun ChatModeScreen(
    uiState: ChatUiState,
    textToSpeech: TextToSpeechInstance?,
    onNavigateToSettings: () -> Unit,
    onOpenMail: () -> Unit = {},
    onOpenCalendar: () -> Unit = {},
    onOpenSearch: () -> Unit = {},
    onOpenCategories: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)?,
) {
    var showWorkFeed by rememberSaveable { mutableStateOf(false) }
    // Hoisted here so the draft survives recompositions that remove QuestionInput
    // from composition and would otherwise drop the text.
    var questionInputText by rememberSaveable(stateSaver = TextFieldValue.Saver) {
        mutableStateOf(TextFieldValue(""))
    }
    val keyboardController = LocalSoftwareKeyboardController.current
    val snackbarHostState = remember { SnackbarHostState() }

    // Left navigation drawer (analysis surfaces): opened by the top-bar
    // hamburger or a left-edge swipe; system back closes it before exiting.
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    val drawerScope = rememberCoroutineScope()
    ai.deneb.PlatformBackHandler(enabled = drawerState.isOpen) {
        drawerScope.launch { drawerState.close() }
    }

    // Right-side session selector: opened by the top-bar session button or a
    // right-edge swipe (mirroring the left drawer); dismissed by scrim or back.
    val sessionDrawerState = rememberDrawerState(DrawerValue.Closed)
    ai.deneb.PlatformBackHandler(enabled = sessionDrawerState.isOpen) {
        drawerScope.launch { sessionDrawerState.close() }
    }
    // Reload the session list whenever the session drawer starts opening, so it
    // reflects sessions created since startup (the list is otherwise loaded once
    // at init and never refreshed — which left a stale drawer).
    LaunchedEffect(drawerState, sessionDrawerState) {
        snapshotFlow {
            drawerState.targetValue == DrawerValue.Open || sessionDrawerState.targetValue == DrawerValue.Open
        }.collect { opening -> if (opening) uiState.actions.refreshConversations() }
    }

    // An edge-swipe opens either drawer without touching the input field, so the
    // soft keyboard would otherwise linger over the drawer content. Hide it the
    // moment either drawer starts opening (targetValue flips to Open) — this
    // covers both the swipe gesture and the top-bar buttons.
    LaunchedEffect(drawerState, sessionDrawerState) {
        snapshotFlow {
            drawerState.targetValue == DrawerValue.Open || sessionDrawerState.targetValue == DrawerValue.Open
        }.collect { opening ->
            if (opening) keyboardController?.hide()
        }
    }

    LaunchedEffect(uiState.snackbarMessage) {
        val resource = uiState.snackbarMessage ?: return@LaunchedEffect
        snackbarHostState.showSnackbar(getString(resource))
        uiState.actions.clearSnackbar()
    }

    val filteredConversations = remember(uiState.savedConversations, uiState.pendingConversationDeletion) {
        val pendingId = uiState.pendingConversationDeletion
        if (pendingId != null) uiState.savedConversations.filter { it.id != pendingId }.toImmutableList() else uiState.savedConversations
    }

    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Rtl) {
        ModalNavigationDrawer(
            drawerState = sessionDrawerState,
            // Desktop-only: the RIGHT session drawer (opened by the session button). On
            // phone the session drawer is the LEFT one (below, hamburger + left swipe),
            // so disable this right drawer's edge gestures there.
            gesturesEnabled = currentPlatform is Platform.Desktop,
            drawerContent = {
                CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                    DenebSessionDrawerSheet(
                        conversations = filteredConversations,
                        currentConversationId = uiState.currentConversationId,
                        pendingConversationDeletion = uiState.pendingConversationDeletion,
                        actions = uiState.actions,
                        onClose = { drawerScope.launch { sessionDrawerState.close() } },
                    )
                }
            },
        ) {
            CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                ModalNavigationDrawer(
                    drawerState = drawerState,
                    // Desktop uses the persistent DenebSidebar (App.kt) instead of this modal drawer;
                    // empty content + gestures off makes the left drawer inert there. Mobile unchanged.
                    gesturesEnabled = currentPlatform !is Platform.Desktop,
                    drawerContent = {
                        // Phone: the LEFT drawer is the session history (GPT/Claude-style),
                        // opened by the hamburger / left-edge swipe — sections moved to the
                        // bottom bar, so the old left nav menu is gone. Desktop uses the
                        // persistent DenebSidebar + the right session drawer, so this is empty.
                        if (currentPlatform !is Platform.Desktop) {
                            DenebSessionDrawerSheet(
                                conversations = filteredConversations,
                                currentConversationId = uiState.currentConversationId,
                                pendingConversationDeletion = uiState.pendingConversationDeletion,
                                actions = uiState.actions,
                                onClose = { drawerScope.launch { drawerState.close() } },
                            )
                        }
                    },
                ) {
                    Box(
                        Modifier
                            .fillMaxSize()
                            .background(MaterialTheme.colorScheme.background)
                            .navigationBarsPadding()
                            .statusBarsPadding()
                            .imePadding(),
                    ) {
                        Column(Modifier.fillMaxSize()) {
                            TopBar(
                                textToSpeech = textToSpeech,
                                isSpeechOutputEnabled = uiState.isSpeechOutputEnabled,
                                isSpeaking = uiState.isSpeaking,
                                actions = uiState.actions,
                                isChatHistoryEmpty = uiState.history.isEmpty(),
                                recallEnabled = uiState.recallEnabled,
                                // Desktop has the persistent sidebar, so no hamburger (null → DrawerButton hides).
                                onOpenDrawer = if (currentPlatform is Platform.Desktop) {
                                    null
                                } else {
                                    { drawerScope.launch { drawerState.open() } }
                                },
                                navigationTabBar = navigationTabBar,
                                // Desktop opens sessions via this button (right drawer); on
                                // phone the hamburger opens sessions (left drawer), so hide it.
                                onOpenSessionDrawer = if (currentPlatform is Platform.Desktop) {
                                    { drawerScope.launch { sessionDrawerState.open() } }
                                } else {
                                    null
                                },
                                onOpenWorkFeed = { showWorkFeed = true },
                                workFeedCount = uiState.workFeed.size,
                            )

                            if (showWorkFeed) {
                                ModalBottomSheet(onDismissRequest = { showWorkFeed = false }) {
                                    WorkFeedPanel(
                                        items = uiState.workFeed,
                                        onOpen = { id ->
                                            showWorkFeed = false
                                            uiState.actions.openWorkFeedItem(id)
                                        },
                                        onRunAction = uiState.actions.runWorkFeedAction,
                                        onClose = { showWorkFeed = false },
                                    )
                                }
                            }

                            HeartbeatBanner(
                                visible = uiState.hasUnreadHeartbeat,
                                onTap = {
                                    uiState.heartbeatConversationId?.let { uiState.actions.loadConversation(it) }
                                    uiState.actions.clearUnreadHeartbeat()
                                },
                                onDismiss = {
                                    uiState.actions.clearUnreadHeartbeat()
                                },
                            )

                            WorkReportBanner(
                                visible = uiState.hasUnreadWorkReport,
                                onTap = {
                                    uiState.actions.openWorkReport()
                                },
                                onDismiss = {
                                    uiState.actions.clearUnreadWorkReport()
                                },
                            )

                            PendingSmsBanners(
                                drafts = uiState.smsDrafts,
                                onSend = uiState.actions.sendSmsDraft,
                                onDiscard = uiState.actions.discardSmsDraft,
                            )

                            uiState.warning?.let { warning ->
                                Text(
                                    text = stringResource(warning),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.error,
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(horizontal = 16.dp, vertical = 8.dp),
                                )
                            }

                            Box(Modifier.weight(1f)) {
                                var isDropping by remember {
                                    mutableStateOf(false)
                                }
                                val addFile by rememberUpdatedState(uiState.actions.addFile)
                                val canAcceptDrop by rememberUpdatedState(uiState.supportedFileExtensions.isNotEmpty())
                                val shouldStartDragAndDrop = remember { { _: DragAndDropEvent -> canAcceptDrop } }
                                val dropTarget = remember {
                                    object : DragAndDropTarget {
                                        override fun onEntered(event: DragAndDropEvent) {
                                            super.onEntered(event)
                                            isDropping = true
                                        }
                                        override fun onExited(event: DragAndDropEvent) {
                                            super.onExited(event)
                                            isDropping = false
                                        }
                                        override fun onDrop(event: DragAndDropEvent): Boolean {
                                            val file = onDragAndDropEventDropped(event)
                                            if (file != null) addFile(file)
                                            isDropping = false
                                            return file != null
                                        }
                                    }
                                }
                                Column(
                                    Modifier
                                        .fillMaxSize()
                                        .blur(radius = if (isDropping) 4.dp else 0.dp)
                                        .dragAndDropTarget(
                                            shouldStartDragAndDrop = shouldStartDragAndDrop,
                                            target = dropTarget,
                                        ),
                                ) {
                                    if (uiState.history.isEmpty()) {
                                        EmptyState(
                                            modifier = Modifier.fillMaxWidth().weight(1f),
                                        )
                                    } else {
                                        // Prefetch ~half a viewport ahead so each expensive markdown item is
                                        // composed + measured before it scrolls into view (off the scroll frame).
                                        // Pausable composition (Compose 1.10+) splits that work across frames —
                                        // the measured bottleneck is markdown measure (~3x a plain Text), exactly
                                        // the "complex list item" case this targets.
                                        val listState = rememberLazyListState(
                                            cacheWindow = LazyLayoutCacheWindow(ahead = 500.dp, behind = 300.dp),
                                        )
                                        val componentScope = rememberCoroutineScope()
                                        // Stable handle hoisted out of the volatile uiState: every streaming
                                        // token emits a new uiState, so a lambda that captures `uiState` gets a
                                        // fresh identity each token and defeats strong-skipping — every visible
                                        // message then recomposes per token while a reply streams. `actions` is a
                                        // fixed reference (created once, carried across emits by state.copy), so
                                        // capturing it instead lets unchanged messages skip during streaming.
                                        val actions = uiState.actions

                                        LaunchedEffect(uiState.history.size) {
                                            // Capture history at effect start to prevent race conditions
                                            val history = uiState.history
                                            if (history.isNotEmpty()) {
                                                listState.scrollToItem(history.lastIndex)
                                                val lastMessage = history.last()
                                                if (uiState.isSpeechOutputEnabled && lastMessage.role == History.Role.ASSISTANT) {
                                                    componentScope.launch(getBackgroundDispatcher()) {
                                                        textToSpeech?.stop()
                                                        uiState.actions.setIsSpeaking(true, lastMessage.id)
                                                        try {
                                                            textToSpeech?.say(lastMessage.content.toSpeakableText())
                                                        } catch (_: TextToSpeechSynthesisInterruptedError) {
                                                            // Speech was interrupted by user
                                                        } catch (_: Exception) {
                                                            // Handle TTS errors gracefully (service failure, audio issues, etc.)
                                                        } finally {
                                                            uiState.actions.setIsSpeaking(false, lastMessage.id)
                                                        }
                                                    }
                                                }
                                            }
                                        }

                                        // Jump-to-report: opening a proactive 업무 card lands on its
                                        // mirrored transcript message instead of the bottom. Declared
                                        // AFTER the bottom-scroll effect above: when one history install
                                        // relaunches both, this one launches last, so its scrollToItem
                                        // preempts the bottom snap and the card position wins. Keyed on
                                        // history too so it retries once the transcript actually
                                        // contains the target (install may land after the request).
                                        val pendingScrollId = uiState.pendingScrollToMessageId
                                        LaunchedEffect(pendingScrollId, uiState.history) {
                                            if (pendingScrollId == null) return@LaunchedEffect
                                            val idx = uiState.history.indexOfFirst { it.id == pendingScrollId }
                                            if (idx >= 0) {
                                                listState.scrollToItem(idx)
                                                actions.consumePendingScroll()
                                            }
                                        }

                                        val lastAssistantId = remember(uiState.history) { uiState.history.lastRenderedAssistant()?.id }
                                        // The streaming caret belongs only on the answer currently being written
                                        // — not on a finished reply while the NEXT turn is still thinking, when
                                        // that finished reply is still the last assistant message. True only once
                                        // an answer sits after the most recent user question.
                                        val isResponseStreaming = remember(uiState.history, uiState.isLoading) {
                                            uiState.isLoading &&
                                                uiState.history.indexOfLast {
                                                    it.role == History.Role.ASSISTANT && !it.isThinking && it.content.isNotEmpty()
                                                } > uiState.history.indexOfLast { it.role == History.Role.USER }
                                        }
                                        // Pair every user submission with its originating assistant so the deneb-ui
                                        // renders once (on the assistant side) with a frozen snapshot — never as a
                                        // separate user-side card. pressedEvent + values persist across the loading
                                        // transition; isPending is only set for the latest in-flight submission.
                                        val pairings = remember(uiState.history, uiState.isLoading) {
                                            val history = uiState.history
                                            val lastUserIdx = history.indexOfLast { it.role == History.Role.USER }
                                            val frozen = mutableMapOf<String, FrozenSubmission>()
                                            val userIdByAssistant = mutableMapOf<String, String>()
                                            for ((i, h) in history.withIndex()) {
                                                if (h.role != History.Role.USER) continue
                                                val sub = h.uiSubmission ?: continue
                                                val originId = (i - 1 downTo 0).firstNotNullOfOrNull { j ->
                                                    history[j].takeIf {
                                                        it.role == History.Role.ASSISTANT &&
                                                            it.content.isNotEmpty() && !it.isThinking &&
                                                            it.content == sub.sourceContent
                                                    }?.id
                                                } ?: (i - 1 downTo 0).firstNotNullOfOrNull { j ->
                                                    history[j].takeIf {
                                                        it.role == History.Role.ASSISTANT &&
                                                            it.content.isNotEmpty() && !it.isThinking
                                                    }?.id
                                                } ?: continue
                                                frozen[originId] = FrozenSubmission(
                                                    values = sub.values,
                                                    pressedEvent = sub.pressedEvent,
                                                    isPending = uiState.isLoading && i == lastUserIdx,
                                                )
                                                userIdByAssistant[originId] = h.id
                                            }
                                            frozen.toMap() to userIdByAssistant.toMap()
                                        }
                                        val frozenByAssistantId = pairings.first
                                        val userIdByAssistantId = pairings.second
                                        val executingToolsState = rememberExecutingTools(uiState.history)

                                        val fallbackStatusText = uiState.fallbackStatus?.let { status ->
                                            val failed = stringResource(Res.string.fallback_service_failed, status.serviceName, uiErrorText(status.errorReason))
                                            val next = status.nextServiceName?.let { stringResource(Res.string.fallback_trying_next, it) }
                                            if (next != null) "$failed\n$next" else failed
                                        }

                                        // Group every reasoning segment in a response (intermediate tool-call /
                                        // thinking-only turns plus the final answer's own reasoning) under the
                                        // answer-bearing assistant message, so each response shows a single
                                        // collapsible "Thinking" section instead of N standalone ones.
                                        val (reasoningSegmentsByAssistantId, suppressedThinkingIds) = remember(uiState.history) {
                                            val byAnswerId = mutableMapOf<String, ImmutableList<String>>()
                                            val suppressed = mutableSetOf<String>()
                                            val pending = mutableListOf<String>()
                                            val pendingThinkingIds = mutableListOf<String>()
                                            for (entry in uiState.history) {
                                                when {
                                                    entry.role == History.Role.USER -> {
                                                        pending.clear()
                                                        pendingThinkingIds.clear()
                                                    }

                                                    entry.role == History.Role.ASSISTANT &&
                                                        entry.isThinking &&
                                                        entry.content.isNotEmpty() -> {
                                                        pending.add(entry.content)
                                                        pendingThinkingIds.add(entry.id)
                                                    }

                                                    entry.role == History.Role.ASSISTANT &&
                                                        !entry.isThinking &&
                                                        entry.content.isNotEmpty() -> {
                                                        val combined = buildList {
                                                            addAll(pending)
                                                            entry.reasoningContent?.takeIf { it.isNotBlank() }?.let { add(it) }
                                                        }
                                                        if (combined.isNotEmpty()) byAnswerId[entry.id] = combined.toImmutableList()
                                                        suppressed.addAll(pendingThinkingIds)
                                                        pending.clear()
                                                        pendingThinkingIds.clear()
                                                    }

                                                    entry.role == History.Role.ASSISTANT &&
                                                        entry.toolCalls != null -> {
                                                        // Assistant turn with tool calls but no answer text yet —
                                                        // capture its reasoning, attach to the eventual answer.
                                                        entry.reasoningContent
                                                            ?.takeIf { it.isNotBlank() }
                                                            ?.let { pending.add(it) }
                                                    }
                                                }
                                            }
                                            // In-flight: the user is still waiting for the answer but earlier
                                            // thinking turns are already in history. Collapse them into the most
                                            // recent thinking entry so the user sees ONE growing Thinking section
                                            // instead of a separate bubble per tool-loop iteration.
                                            if (pendingThinkingIds.isNotEmpty()) {
                                                val lastId = pendingThinkingIds.last()
                                                byAnswerId[lastId] = pending.toImmutableList()
                                                for (i in 0 until pendingThinkingIds.size - 1) {
                                                    suppressed.add(pendingThinkingIds[i])
                                                }
                                            }
                                            byAnswerId to suppressed
                                        }

                                        val showScrollToBottom by remember {
                                            derivedStateOf {
                                                val lastVisibleItem = listState.layoutInfo.visibleItemsInfo.lastOrNull()
                                                lastVisibleItem != null && lastVisibleItem.index < listState.layoutInfo.totalItemsCount - 1
                                            }
                                        }

                                        // A subtle buzz when a reply finishes — chief-of-staff replies can be
                                        // long, so signal "done" even if the user has looked away.
                                        val haptics = rememberHaptics()
                                        val wasLoading = remember { mutableStateOf(false) }
                                        // Anchor for the waiting chip's elapsed display: marks the turn's
                                        // actual start, so the count survives the chip briefly leaving
                                        // composition (deneb-ui pending stretch).
                                        val turnStart = remember { mutableStateOf<TimeSource.Monotonic.ValueTimeMark?>(null) }
                                        LaunchedEffect(uiState.isLoading) {
                                            if (wasLoading.value && !uiState.isLoading) haptics.tap()
                                            wasLoading.value = uiState.isLoading
                                            turnStart.value = if (uiState.isLoading) TimeSource.Monotonic.markNow() else null
                                        }

                                        // Follow the stream: while a reply streams in, keep the newest tokens
                                        // in view — but only when the user is already near the bottom, so
                                        // scrolling up to re-read earlier text isn't yanked back down.
                                        val isNearBottom by remember {
                                            derivedStateOf {
                                                val info = listState.layoutInfo
                                                val last = info.visibleItemsInfo.lastOrNull() ?: return@derivedStateOf true
                                                last.index >= info.totalItemsCount - 1 &&
                                                    last.offset + last.size <= info.viewportEndOffset + 240
                                            }
                                        }
                                        val streamingLen = uiState.history.lastOrNull()?.content?.length ?: 0
                                        LaunchedEffect(streamingLen, uiState.isLoading) {
                                            if (uiState.isLoading && isNearBottom) {
                                                val total = listState.layoutInfo.totalItemsCount
                                                if (total > 0) listState.scrollToItem(total - 1, Int.MAX_VALUE)
                                            }
                                        }

                                        Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
                                            LazyColumn(
                                                modifier = Modifier.fillMaxSize(),
                                                state = listState,
                                                horizontalAlignment = CenterHorizontally,
                                                // Breathing room so the first message clears the top bar and the
                                                // last clears the input bar instead of sitting flush against them.
                                                contentPadding = PaddingValues(top = 4.dp, bottom = 16.dp),
                                            ) {
                                                items(uiState.history, key = { it.id }, contentType = { it.role }) { history ->
                                                    // Readable measure on a wide desktop window: cap every row at the
                                                    // shared content width (no-op on phone, where this fills the width).
                                                    // The list itself stays full width so the mouse wheel works from the
                                                    // margins and the scrollbar hugs the pane edge; the LazyColumn's
                                                    // CenterHorizontally centers the capped rows.
                                                    Column(denebContentWidthModifier()) {
                                                        when (history.role) {
                                                            History.Role.USER -> {
                                                                // Submissions are shown by the paired assistant's frozen deneb-ui card
                                                                // above; the "Responded with: …" text bubble would be redundant.
                                                                if (history.uiSubmission == null) {
                                                                    UserMessage(
                                                                        message = history.content,
                                                                        attachments = history.attachments,
                                                                    )
                                                                }
                                                            }

                                                            History.Role.ASSISTANT -> {
                                                                if ((history.content.isNotEmpty() || history.attachments.isNotEmpty()) && !history.isThinking) {
                                                                    val isLastAssistant = history.id == lastAssistantId
                                                                    val frozen = frozenByAssistantId[history.id]
                                                                    val pairedUserId = userIdByAssistantId[history.id]
                                                                    BotMessage(
                                                                        message = history.content,
                                                                        attachments = history.attachments,
                                                                        textToSpeech = textToSpeech,
                                                                        isSpeaking = uiState.isSpeaking && uiState.isSpeakingContentId == history.id,
                                                                        setIsSpeaking = {
                                                                            actions.setIsSpeaking(it, history.id)
                                                                        },
                                                                        onRegenerate = if (isLastAssistant) actions.regenerate else null,
                                                                        isInteractive = isLastAssistant && !uiState.isLoading && frozen == null,
                                                                        onUiCallback = { event, data ->
                                                                            actions.submitUiCallback(event, data)
                                                                        },
                                                                        frozen = frozen,
                                                                        onResubmit = if (pairedUserId != null && !uiState.isLoading) {
                                                                            { event, data -> actions.resubmit(pairedUserId, event, data) }
                                                                        } else {
                                                                            null
                                                                        },
                                                                        reasoningSegments = reasoningSegmentsByAssistantId[history.id] ?: persistentListOf(),
                                                                        isStreaming = isLastAssistant && isResponseStreaming,
                                                                    )
                                                                    if (history.toolFootprint != null) {
                                                                        androidx.compose.material3.Text(
                                                                            text = stringResource(Res.string.tool_footprint, history.toolFootprint),
                                                                            style = MaterialTheme.typography.labelSmall,
                                                                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                                            modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
                                                                        )
                                                                    }
                                                                    if (history.fallbackServiceName != null) {
                                                                        androidx.compose.material3.Text(
                                                                            text = stringResource(Res.string.fallback_answered_by, history.fallbackServiceName),
                                                                            style = MaterialTheme.typography.labelSmall,
                                                                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                                            modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
                                                                        )
                                                                    }
                                                                } else if (history.isThinking &&
                                                                    history.content.isNotEmpty() &&
                                                                    history.id !in suppressedThinkingIds
                                                                ) {
                                                                    // Thinking-only turn still in flight — render as a standalone
                                                                    // reasoning bubble. The precomputation above has already gathered
                                                                    // every earlier thinking segment in this cycle under this id.
                                                                    BotMessage(
                                                                        message = "",
                                                                        textToSpeech = null,
                                                                        isSpeaking = false,
                                                                        setIsSpeaking = {},
                                                                        reasoningSegments = reasoningSegmentsByAssistantId[history.id]
                                                                            ?: persistentListOf(history.content),
                                                                    )
                                                                }
                                                            }

                                                            History.Role.TOOL_EXECUTING -> {
                                                                // Rendered in WaitingResponseRow below
                                                            }

                                                            History.Role.TOOL -> {
                                                                // Don't show completed tool results in UI
                                                            }
                                                        }
                                                    }
                                                }
                                                // Skip the generic "thinking" row during a pending deneb-ui submission — the
                                                // pressed button's pulse already signals work in flight. Keep it for tool
                                                // activity so tool feedback isn't lost.
                                                val showWaitingRow = uiState.isLoading &&
                                                    (frozenByAssistantId.values.none { it.isPending } || executingToolsState.tools.isNotEmpty())
                                                if (showWaitingRow) {
                                                    item(key = "loading") {
                                                        Column(denebContentWidthModifier()) {
                                                            WaitingResponseRow(
                                                                executingTools = executingToolsState.tools,
                                                                isStatusOnly = executingToolsState.isStatusOnly,
                                                                statusText = fallbackStatusText,
                                                                turnStart = turnStart.value,
                                                            )
                                                        }
                                                    }
                                                }
                                                uiState.error?.let { error ->
                                                    item(key = "error") {
                                                        Column(denebContentWidthModifier()) {
                                                            ErrorMessage(error = error, retry = uiState.actions.retry)
                                                        }
                                                    }
                                                }
                                            }

                                            VerticalScrollbarForList(
                                                listState = listState,
                                                modifier = Modifier.align(CenterEnd).fillMaxHeight(),
                                            )

                                            androidx.compose.animation.AnimatedVisibility(
                                                visible = showScrollToBottom,
                                                modifier = Modifier.align(BottomCenter).padding(bottom = 8.dp),
                                                enter = denebPopEnter,
                                                exit = denebPopExit,
                                            ) {
                                                SmallFloatingActionButton(
                                                    modifier = Modifier
                                                        .handCursor(),
                                                    onClick = {
                                                        componentScope.launch {
                                                            val totalItems = listState.layoutInfo.totalItemsCount
                                                            if (totalItems > 0) {
                                                                listState.animateScrollToItem(totalItems - 1)
                                                            }
                                                        }
                                                    },
                                                ) {
                                                    Icon(Icons.Default.KeyboardArrowDown, contentDescription = stringResource(Res.string.scroll_to_bottom_content_description))
                                                }
                                            }
                                        }
                                    }
                                }
                            }

                            // Same cap as the message rows so the input bar lines up with the
                            // conversation column on desktop instead of spanning the whole pane.
                            Box(Modifier.fillMaxWidth(), contentAlignment = TopCenter) {
                                Column(denebContentWidthModifier()) {
                                    QuestionInput(
                                        files = uiState.files,
                                        addFile = uiState.actions.addFile,
                                        removeFile = uiState.actions.removeFile,
                                        ask = uiState.actions.ask,
                                        supportedFileExtensions = uiState.supportedFileExtensions,
                                        textState = questionInputText,
                                        onTextStateChange = { questionInputText = it },
                                        isLoading = uiState.isLoading,
                                        cancel = uiState.actions.cancel,
                                        availableServices = uiState.availableServices,
                                        onSelectService = uiState.actions.selectService,
                                    )
                                }
                            }
                        }
                        SnackbarHost(
                            hostState = snackbarHostState,
                            modifier = Modifier.align(BottomCenter).padding(bottom = 80.dp),
                        ) { data ->
                            Snackbar(snackbarData = data)
                        }
                    }
                } // ModalNavigationDrawer (left)
            } // CompositionLocalProvider Ltr (content)
        } // ModalNavigationDrawer (right session drawer)
    } // CompositionLocalProvider Rtl
}

private data class ExecutingToolsState(
    val tools: ImmutableList<Pair<String, String>>,
    val isStatusOnly: Boolean,
)

@Composable
private fun rememberExecutingTools(history: ImmutableList<History>): ExecutingToolsState {
    // Wrap the history parameter in State so derivedStateOf can observe it, then
    // only recompute (and only emit) when the executing-tools subset actually changes.
    // Streaming tokens mutate `history` on every frame but rarely change this derived slice.
    val historyState = rememberUpdatedState(history)
    val state by remember {
        derivedStateOf {
            val executing = historyState.value.filter { it.role == History.Role.TOOL_EXECUTING }
            ExecutingToolsState(
                tools = executing.map { it.id to (it.toolName ?: "tool") }.toImmutableList(),
                isStatusOnly = executing.any { it.isStatusMessage },
            )
        }
    }
    return state
}
