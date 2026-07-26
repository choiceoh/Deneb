@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat.composables

import ai.deneb.deneb.DenebLoading
import ai.deneb.getBackgroundDispatcher
import ai.deneb.onDragAndDropEventDropped
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.ChatUiState
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.lastRenderedAssistant
import ai.deneb.ui.components.VerticalScrollbarForList
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebContentWidthModifier
import ai.deneb.ui.denebFadeEnter
import ai.deneb.ui.denebFadeExit
import ai.deneb.ui.denebSnappySpring
import ai.deneb.ui.denebSpatialSpring
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.precomputeMarkdownAsync
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.draganddrop.dragAndDropTarget
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.gestures.scrollBy
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
import androidx.compose.foundation.lazy.LazyListState
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
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.withFrameNanos
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
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
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
                        // Land on the true last row (trailing NON-history rows — the
                        // "loading" waiting row, the "error" row — sit below the last
                        // message, so history.lastIndex isn't the list's last row) AND
                        // clear the bottom contentPadding so the newest line rests just
                        // above the floating input bar. scrollToTrueBottom handles both.
                        listState.scrollToTrueBottom(bottomOverlayHeightPx)
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
            // 마지막 사용자 메시지만 편집-재전송 대상 (서버 transcript truncation 부재 —
            // regenerate와 같은 로컬 되감기 시맨틱이 정직하게 성립하는 유일한 위치).
            val lastUserId = remember(uiState.history) {
                uiState.history.lastOrNull { it.role == History.Role.USER }?.id
            }
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

            // One action row per RESPONSE, not per fragment. A single answer arrives
            // as several assistant messages (text, tool call, more text, …), and every
            // fragment was drawing its own 복사/⋯ row — a turn showed the same two
            // icons three or four times down the thread. Same grouping the reasoning
            // block above already does for the very same reason.
            //
            // The row lands on the LAST answer-bearing message of the response, and it
            // carries the WHOLE response's text: copy on the tail fragment alone would
            // silently hand over a fraction of the answer.
            val (actionRowIds, responseTextById) = remember(uiState.history) {
                responseActionRows(uiState.history)
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
                    // Land on the true last row AND clear the input-bar contentPadding
                    // so the streaming tokens' newest line isn't clipped under the bar.
                    listState.scrollToTrueBottom(bottomOverlayHeightPx)
                }
            }

            // Keyboard follow-scroll: the root Box's imePadding shrinks this list's
            // viewport as the keyboard opens. LazyColumn pins the top item on a resize,
            // so the newest message slides under the input bar. Track the viewport
            // height (the source of truth — NOT raw WindowInsets.ime, which double-
            // counts against imePadding's consumed nav-bar overlap) and scroll the list
            // by exactly the px it lost.
            //
            // scrollBy alone isn't enough: at the list's scroll limit (last message
            // near the end) it can't advance the full delta, and the bottom
            // contentPadding (input-bar height) is consumed first — leaving the last
            // message a few px shy (#3537). So after the frame-by-frame scrollBy tracks
            // the keyboard's own curve, a scrollToItem(last) with a negative offset
            // snaps the newest message to rest exactly above the input bar, clearing
            // whatever the bounded scrollBy couldn't. snapshotFlow keeps this on the
            // effect coroutine (no per-frame recomposition); near-bottom only, so a
            // user scrolled up to re-read isn't yanked.
            LaunchedEffect(listState, bottomOverlayHeightPx) {
                var prevHeight = listState.layoutInfo.viewportSize.height
                snapshotFlow { listState.layoutInfo.viewportSize.height }
                    .collect { current ->
                        val shrinkage = prevHeight - current
                        prevHeight = current
                        if (shrinkage > 0 && isNearBottom) {
                            // Ride the keyboard's animation curve frame-by-frame. scrollBy
                            // returns how far it actually advanced — at the list's scroll
                            // limit (last message near the end) it falls short of the full
                            // delta, and the bottom contentPadding (input-bar height) is
                            // consumed first. When that happens, snap the newest message to
                            // rest exactly above the input bar instead of leaving it a few
                            // px shy (#3537). The snap only fires on the shortfall, so the
                            // common case tracks the keyboard smoothly frame-by-frame.
                            val advanced = listState.scrollBy(shrinkage.toFloat())
                            if (advanced < shrinkage) {
                                val total = listState.layoutInfo.totalItemsCount
                                if (total > 0) {
                                    listState.scrollToItem(total - 1, -bottomOverlayHeightPx)
                                }
                            }
                        }
                    }
            }

            Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
                LazyColumn(
                    // Soft fade at the top/bottom edges so a message dissolves into
                    // the bars as it scrolls past, instead of reading as hard-cut /
                    // covered. The chat still fills the full height (small padding,
                    // not a wide gap) — it just flows under the bars, uncovered.
                    //
                    // Deliberately a SLIVER, not the bar's full height. Widening these
                    // to the measured overlay heights (56dp / 80dp) was tried and
                    // reverted: a whole line of text then sits at ~50% alpha, and a
                    // crisp → ghost → crisp stack reads as a rendering fault rather
                    // than a dissolve. A short band only ever catches a glyph's edge.
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
                        //
                        // animateItem: a newly inserted row (my sent bubble, the reply row)
                        // fades in and settles into place instead of popping — the send
                        // moment reads as continuous. First composition doesn't animate, so
                        // a cold transcript load stays instant; a session switch fades only
                        // the ~visible rows (keys change), which reads as a soft page-in.
                        Column(
                            denebContentWidthModifier().animateItem(
                                fadeInSpec = denebSnappySpring(),
                                placementSpec = denebSpatialSpring(),
                                fadeOutSpec = denebSnappySpring(),
                            ),
                        ) {
                            when (history.role) {
                                History.Role.USER -> {
                                    // Submissions are shown by the paired assistant's frozen deneb-ui card
                                    // above; the "Responded with: …" text bubble would be redundant.
                                    if (history.uiSubmission == null) {
                                        UserMessage(
                                            message = history.content,
                                            attachments = history.attachments,
                                            timestampMs = history.timestampMs,
                                            onEditResend = if (history.id == lastUserId && !uiState.isLoading) {
                                                actions.editResendLast
                                            } else {
                                                null
                                            },
                                        )
                                    }
                                }

                                History.Role.ASSISTANT -> {
                                    if ((history.content.isNotEmpty() || history.attachments.isNotEmpty()) && !history.isThinking) {
                                        val isLastAssistant = history.id == lastAssistantId
                                        val frozen = frozenByAssistantId[history.id]
                                        val pairedUserId = userIdByAssistantId[history.id]
                                        // ‹ n/N › — an older variant selected? Swap the DISPLAYED
                                        // body only; streaming/frozen/reasoning stay keyed to the
                                        // live row (variants are settled answers by construction).
                                        val variantShown = if (
                                            isLastAssistant &&
                                            uiState.lastAnswerVariantIndex < uiState.lastAnswerVariants.size
                                        ) {
                                            uiState.lastAnswerVariants[uiState.lastAnswerVariantIndex]
                                        } else {
                                            null
                                        }
                                        BotMessage(
                                            message = (variantShown ?: history).content,
                                            attachments = (variantShown ?: history).attachments,
                                            timestampMs = (variantShown ?: history).timestampMs,
                                            textToSpeech = textToSpeech,
                                            isSpeaking = uiState.isSpeaking && uiState.isSpeakingContentId == history.id,
                                            setIsSpeaking = {
                                                actions.setIsSpeaking(it, history.id)
                                            },
                                            onRegenerate = if (isLastAssistant) actions.regenerate else null,
                                            // Intermediate fragments of the same answer carry no row;
                                            // the tail carries one for the whole response.
                                            showActions = history.id in actionRowIds,
                                            copyText = responseTextById[history.id],
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
                                            variantNav = if (
                                                isLastAssistant && uiState.lastAnswerVariants.isNotEmpty() && !uiState.isLoading
                                            ) {
                                                VariantNav(
                                                    index = uiState.lastAnswerVariantIndex,
                                                    total = uiState.lastAnswerVariants.size + 1,
                                                    onSelect = actions.selectAnswerVariant,
                                                )
                                            } else {
                                                null
                                            },
                                        )
                                        if (history.id == uiState.stoppedMessageId) {
                                            // The user stopped this answer mid-stream;
                                            // mark it so a half-reply doesn't read as
                                            // complete (regenerate is on the last one).
                                            androidx.compose.material3.Text(
                                                text = "중단됨",
                                                style = DenebType.meta,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
                                            )
                                        }
                                        if (history.toolFootprint != null) {
                                            androidx.compose.material3.Text(
                                                text = stringResource(Res.string.tool_footprint, history.toolFootprint),
                                                style = DenebType.meta,
                                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                                modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
                                            )
                                        }
                                        if (history.fallbackServiceName != null) {
                                            androidx.compose.material3.Text(
                                                text = stringResource(Res.string.fallback_answered_by, history.fallbackServiceName),
                                                style = DenebType.meta,
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
                    // Inset by the same measured bars: a full-height track ran on
                    // past the conversation and left a grey thumb floating beside
                    // the input field.
                    modifier = Modifier.align(CenterEnd).fillMaxHeight().padding(
                        top = with(topOverlayDensity) { topOverlayHeightPx.toDp() },
                        bottom = with(topOverlayDensity) { bottomOverlayHeightPx.toDp() },
                    ),
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
                                // Lands on the true bottom AND clears the input-bar contentPadding
                                // (animate, since this is a user tap) so the newest line rests just
                                // above the bar — not clipped under it. No-ops when the list is empty.
                                listState.scrollToTrueBottom(bottomOverlayHeightPx, animate = true)
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

/**
 * Scroll the list so the true last row's bottom edge rests exactly above the
 * floating input bar — i.e. the last visible line isn't clipped under it.
 *
 * [LazyListState.scrollToItem] with `Int.MAX_VALUE` pins the last item's bottom
 * to the viewport's physical bottom, which IGNORES the list's bottom
 * contentPadding (the input-bar height). The newest line then sits under the
 * floating input bar ("완벽하게 밑까지 안 내려옴"). After pinning the item's
 * bottom, scroll DOWN by the contentPadding so the last line clears the bar.
 *
 * `animate` picks [androidx.compose.foundation.lazy.LazyListState.animateScrollBy]
 * for the user-driven scroll-to-bottom button (smooth) vs the instant
 * [androidx.compose.foundation.lazy.LazyListState.scrollBy] for programmatic
 * follow-scrolls (streaming / on-send) that already ride their own cadence.
 */
private suspend fun LazyListState.scrollToTrueBottom(
    contentPaddingBottomPx: Int,
    animate: Boolean = false,
) {
    // On a cold chat-screen open the install scroll effect can fire before the
    // LazyColumn's first measure, when totalItemsCount is still 0. Wait one layout
    // pass so the scroll isn't a no-op that leaves the list pinned at the top
    // (#3554 regression). Only the non-empty-history branch reaches this call, so
    // items WILL appear; the await cancels with the effect if the screen leaves
    // composition, so an empty list can't hang here.
    if (layoutInfo.totalItemsCount <= 0) {
        snapshotFlow { layoutInfo.totalItemsCount }.first { it > 0 }
    }

    if (animate) {
        // Scroll-to-bottom button: the list is already measured (the button only
        // shows once items are laid out), so one smooth scroll suffices.
        scrollToItem(layoutInfo.totalItemsCount - 1, Int.MAX_VALUE)
        if (contentPaddingBottomPx > 0) animateScrollBy(contentPaddingBottomPx.toFloat())
        return
    }

    // Instant path (cold-entry install / on-send / streaming follow). scrollToItem
    // (last, MAX) pins the last item's bottom to the physical viewport bottom;
    // scrollBy then reveals the trailing contentPadding (input-bar reserve) so the
    // last line rests just above the bar, not clipped under it.
    //
    // A tall last message measures its markdown body over several frames (async
    // precompute + pausable composition). At the first pin that layout is still
    // incomplete, so it reads as "already at the end" and the pin lands short —
    // the newest lines stay hidden below the fold ("완벽하게 밑까지 안 내려옴").
    // canScrollForward can't distinguish this from a genuine bottom. Re-pin each
    // frame for a brief window so the scroll tracks the growing content down to
    // the true bottom; it settles to a no-op once measurement completes.
    repeat(8) {
        scrollToItem(layoutInfo.totalItemsCount - 1, Int.MAX_VALUE)
        if (contentPaddingBottomPx > 0) scrollBy(contentPaddingBottomPx.toFloat())
        withFrameNanos { }
    }
}

/**
 * Which assistant messages carry the 복사/⋯ row, and what 복사 should copy.
 *
 * One answer arrives as SEVERAL assistant messages (text → tool call → more
 * text), and drawing the row under each fragment repeated the same two icons
 * three or four times in a single turn. The row belongs to the RESPONSE, which
 * is the grouping the reasoning block in this file already applies for the very
 * same reason.
 *
 * Returns the id of each response's LAST answer-bearing message, mapped to the
 * whole response's text — copying from the tail fragment alone would silently
 * hand over only the closing paragraph.
 */
internal fun responseActionRows(history: List<History>): Pair<Set<String>, Map<String, String>> {
    val rowIds = mutableSetOf<String>()
    val textById = mutableMapOf<String, String>()
    val parts = mutableListOf<String>()
    var lastAnswerId: String? = null

    fun closeResponse() {
        val id = lastAnswerId ?: return
        rowIds.add(id)
        textById[id] = parts.joinToString("\n\n")
        parts.clear()
        lastAnswerId = null
    }

    for (entry in history) {
        if (entry.role == History.Role.USER) {
            closeResponse()
            continue
        }
        // Thinking-only turns and empty tool-call turns are not answer text.
        if (entry.role == History.Role.ASSISTANT && !entry.isThinking && entry.content.isNotEmpty()) {
            parts.add(entry.content)
            lastAnswerId = entry.id
        }
    }
    closeResponse()
    return rowIds to textById
}
