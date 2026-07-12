@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat.composables

import ai.deneb.deneb.DenebLoading
import ai.deneb.getBackgroundDispatcher
import ai.deneb.onDragAndDropEventDropped
import ai.deneb.ui.chat.ChatUiState
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.lastRenderedAssistant
import ai.deneb.ui.components.VerticalScrollbarForList
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebContentWidthModifier
import ai.deneb.ui.denebFadeEnter
import ai.deneb.ui.denebFadeExit
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.precomputeMarkdownAsync
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.draganddrop.dragAndDropTarget
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.layout.LazyLayoutCacheWindow
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SmallFloatingActionButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment.Companion.BottomCenter
import androidx.compose.ui.Alignment.Companion.CenterEnd
import androidx.compose.ui.Alignment.Companion.CenterHorizontally
import androidx.compose.ui.Modifier
import androidx.compose.ui.draganddrop.DragAndDropEvent
import androidx.compose.ui.draganddrop.DragAndDropTarget
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.Dp
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
import kotlinx.coroutines.launch
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.errors.TextToSpeechSynthesisInterruptedError
import org.jetbrains.compose.resources.stringResource
import kotlin.time.TimeSource

/**
 * The conversation list: drag-and-drop surface, message history, the waiting /
 * error rows, the scrollbar, and the scroll-to-bottom button. Split out of
 * ChatModeScreen.kt so the list's many scroll/derived-state effects can grow
 * without re-bloating the entry file.
 */
@Composable
internal fun ChatMessageList(
    uiState: ChatUiState,
    textToSpeech: TextToSpeechInstance?,
    topOverlayDensity: Density,
    topOverlayHeightPx: Int,
    bottomOverlayHeightPx: Int,
    modifier: Modifier = Modifier,
) {
    val keyboardController = LocalSoftwareKeyboardController.current
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
        modifier
            .fillMaxSize()
            .blur(radius = if (isDropping) 4.dp else 0.dp)
            .dragAndDropTarget(
                shouldStartDragAndDrop = shouldStartDragAndDrop,
                target = dropTarget,
            ),
    ) {
        if (uiState.history.isEmpty()) {
            if (uiState.isRestoring) {
                // Cold open: a long transcript is restored off the main
                // thread (see ChatViewModel init). Show the loading
                // skeleton instead of the greeting so a returning user
                // doesn't see a false "empty chat" flash before it fills.
                Column(Modifier.fillMaxWidth().weight(1f)) {
                    DenebLoading()
                }
            } else {
                EmptyState(
                    modifier = Modifier.fillMaxWidth().weight(1f),
                )
            }
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

            // Drop the soft keyboard when the user drags the conversation — the
            // standard "scroll to read → keyboard out of the way" gesture, paired
            // with the dismiss-on-send in QuestionInput. collectIsDraggedAsState
            // fires only on a USER drag (not the programmatic scroll-to-bottom
            // below, so the two never fight) and observes drag state rather than
            // intercepting pointer events, so it can't break taps, text selection,
            // links, or scrolling.
            val listDragged = listState.interactionSource.collectIsDraggedAsState()
            LaunchedEffect(listDragged.value) {
                if (listDragged.value) keyboardController?.hide()
            }

            // Stable handle hoisted out of the volatile uiState: every streaming
            // token emits a new uiState, so a lambda that captures `uiState` gets a
            // fresh identity each token and defeats strong-skipping — every visible
            // message then recomposes per token while a reply streams. `actions` is a
            // fixed reference (created once, carried across emits by state.copy), so
            // capturing it instead lets unchanged messages skip during streaming.
            val actions = uiState.actions

            // Scroll-on-append guard state: the newest USER message id the effect
            // below has seen. A changed id = the user just sent (or a different
            // session's history installed) → always snap to bottom. An unchanged id
            // = a message arrived on its own (a mirrored work report, an error row)
            // → follow only when already near the bottom, so scrolling up to read
            // older history isn't yanked back down by an unrelated arrival.
            var lastSeenUserMessageId by remember { mutableStateOf<String?>(null) }
            var historyEverInstalled by remember { mutableStateOf(false) }

            LaunchedEffect(uiState.history.size) {
                // Capture history at effect start to prevent race conditions
                val history = uiState.history
                if (history.isNotEmpty()) {
                    val lastUserId = history.lastOrNull { it.role == History.Role.USER }?.id
                    val ownSendOrInstall = !historyEverInstalled ||
                        (lastUserId != null && lastUserId != lastSeenUserMessageId)
                    historyEverInstalled = true
                    lastSeenUserMessageId = lastUserId
                    // Near-bottom at effect time: layoutInfo can predate the append
                    // (totalItemsCount lags a frame), so tolerate the just-added item
                    // (-2) plus the same 240px slack the streaming follow uses.
                    val info = listState.layoutInfo
                    val lastVisible = info.visibleItemsInfo.lastOrNull()
                    val nearBottom = lastVisible == null || (
                        lastVisible.index >= info.totalItemsCount - 2 &&
                            lastVisible.offset + lastVisible.size <= info.viewportEndOffset + 240
                        )
                    if (ownSendOrInstall || nearBottom) {
                        // Pin the newest item to the BOTTOM (Int.MAX_VALUE offset), not
                        // its top — a fresh send/reply should rest just above the input
                        // bar like every chat. Matches the streaming autoscroll and the
                        // scroll-to-bottom button.
                        listState.scrollToItem(history.lastIndex, Int.MAX_VALUE)
                        // The list carries trailing NON-history rows below the last
                        // message — the "loading" waiting row while a reply is pending,
                        // the "error" row — so right after a send, history.lastIndex is
                        // not the list's last row and the waiting row stays cut off
                        // under the input bar ("완전히 밑까지 안 내려옴"). The first snap
                        // has composed/measured the tail; re-snap to the true last row,
                        // same as the streaming follow and the scroll-to-bottom button.
                        val lastRow = listState.layoutInfo.totalItemsCount - 1
                        if (lastRow > history.lastIndex) {
                            listState.scrollToItem(lastRow, Int.MAX_VALUE)
                        }
                    }
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
            // Use the device's spare cores: parse every finished assistant body in the
            // background (Dispatchers.Default, parallel) so scrolling rich history never
            // parses markdown on the UI frame — the composition's parseMarkdownCached then
            // hits a warm cache. The live streaming answer is skipped (it'd churn the LRU).
            LaunchedEffect(uiState.history.size, isResponseStreaming) {
                val bodies = uiState.history.asSequence()
                    .filter {
                        it.role == History.Role.ASSISTANT &&
                            it.content.isNotEmpty() && !it.isThinking
                    }
                    .filterNot { isResponseStreaming && it.id == lastAssistantId }
                    .map { it.content }
                    .toList()
                precomputeMarkdownAsync(bodies)
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
                    // While the list is actively scrolling, fade the jump button out
                    // (화면이 움직일 땐 점점 사라지게) — it fades back in once the list
                    // settles, if the bottom is still out of view.
                    if (listState.isScrollInProgress) return@derivedStateOf false
                    val info = listState.layoutInfo
                    val last = info.visibleItemsInfo.lastOrNull()
                    // Show whenever the conversation's bottom isn't in view — either the
                    // last item isn't composed yet, OR it is but its bottom edge sits
                    // below the viewport (a tall last message the user scrolled up within).
                    // An index-only check missed the latter; GPT/Claude/Gemini show the
                    // jump button in both cases.
                    last != null && (
                        last.index < info.totalItemsCount - 1 ||
                            last.offset + last.size > info.viewportEndOffset
                        )
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
            // in view — yet only when the user is already near the bottom, so
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
            // Coalesce the follow-scroll: the streaming reply's text is sampled
            // ~15×/s, so keying on the raw length would fire scrollToItem on every
            // emission. Bucketing by ~48 chars (≈1–2 lines) drops that to a few
            // snaps/sec — invisibly smooth given the 240px near-bottom slack above,
            // and far less layout churn on the hot streaming path.
            LaunchedEffect(streamingLen / 48, uiState.isLoading) {
                if (uiState.isLoading && isNearBottom) {
                    val total = listState.layoutInfo.totalItemsCount
                    if (total > 0) listState.scrollToItem(total - 1, Int.MAX_VALUE)
                }
            }

            // Keyboard handling moved off scrollBy: imePadding now lives on the
            // input bar, whose measured height (bar + IME) feeds this list's
            // bottom contentPadding. LazyColumn self-aligns the last message
            // above the bar as the keyboard opens — no manual scrollBy needed
            // (it couldn't clear the scroll limit / contentPadding slack and
            // left the last message a few px shy).

            Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
                LazyColumn(
                    // Soft fade at the top/bottom edges so a message dissolves into
                    // the bars as it scrolls past, instead of reading as hard-cut /
                    // covered. The chat still fills the full height (small padding,
                    // not a wide gap) — it just flows under the bars, uncovered.
                    modifier = Modifier.fillMaxSize().verticalEdgeFade(top = 10.dp, bottom = 22.dp),
                    state = listState,
                    horizontalAlignment = CenterHorizontally,
                    // Top inset = the floating overlay's measured height (status
                    // bar + top bar + any banners) so the first message rests just
                    // below the bar; older messages scroll up behind it (immersive).
                    contentPadding = PaddingValues(
                        top = with(topOverlayDensity) { topOverlayHeightPx.toDp() },
                        // Bottom inset = the floating input bar's measured height so the
                        // last message rests just above it; older messages scroll behind.
                        bottom = with(topOverlayDensity) { bottomOverlayHeightPx.toDp() },
                    ),
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
                                                if (event == "choice") {
                                                    data["text"]?.takeIf { it.isNotBlank() }?.let(actions.ask)
                                                } else {
                                                    actions.submitUiCallback(event, data)
                                                }
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
                                        if (history.id == uiState.stoppedMessageId) {
                                            // The user stopped this answer mid-stream;
                                            // mark it so a half-reply doesn't read as
                                            // complete (regenerate is on the last one).
                                            androidx.compose.material3.Text(
                                                text = "중단됨",
                                                style = MaterialTheme.typography.labelSmall,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
                                            )
                                        }
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
                    enter = denebFadeEnter,
                    exit = denebFadeExit,
                ) {
                    SmallFloatingActionButton(
                        modifier = Modifier
                            .handCursor(),
                        onClick = {
                            componentScope.launch {
                                val totalItems = listState.layoutInfo.totalItemsCount
                                if (totalItems > 0) {
                                    // Land on the true bottom, not the last item's top: a single
                                    // tall final message (e.g. a long report) would otherwise leave
                                    // its top pinned to the viewport — the button would appear to do
                                    // nothing. The large scrollOffset pins the item's bottom edge to
                                    // the viewport bottom (same idiom as the streaming follow above).
                                    listState.animateScrollToItem(totalItems - 1, Int.MAX_VALUE)
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

// verticalEdgeFade fades the composable's own content to transparent over [top]
// at the top and [bottom] at the bottom, so a scrolling list dissolves into the
// surrounding bars instead of cutting hard against them. Offscreen layer + a
// DstIn alpha mask: only the gradient's alpha matters, not its colour.
private fun Modifier.verticalEdgeFade(top: Dp, bottom: Dp): Modifier = this
    .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
    .drawWithContent {
        drawContent()
        val topPx = top.toPx()
        if (topPx > 0f) {
            drawRect(
                brush = Brush.verticalGradient(listOf(Color.Transparent, Color.Black), startY = 0f, endY = topPx),
                blendMode = BlendMode.DstIn,
            )
        }
        val bottomPx = bottom.toPx()
        if (bottomPx > 0f) {
            drawRect(
                brush = Brush.verticalGradient(
                    listOf(Color.Black, Color.Transparent),
                    startY = this@drawWithContent.size.height - bottomPx,
                    endY = this@drawWithContent.size.height,
                ),
                blendMode = BlendMode.DstIn,
            )
        }
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
