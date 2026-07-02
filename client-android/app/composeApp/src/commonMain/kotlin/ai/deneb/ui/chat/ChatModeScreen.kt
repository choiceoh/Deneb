@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat

import ai.deneb.deneb.DenebLoading
import ai.deneb.getBackgroundDispatcher
import ai.deneb.onDragAndDropEventDropped
import ai.deneb.ui.chat.composables.BotMessage
import ai.deneb.ui.chat.composables.DenebSessionDrawerSheet
import ai.deneb.ui.chat.composables.EmptyState
import ai.deneb.ui.chat.composables.ErrorMessage
import ai.deneb.ui.chat.composables.HeartbeatBanner
import ai.deneb.ui.chat.composables.PendingSmsBanners
import ai.deneb.ui.chat.composables.QuestionInput
import ai.deneb.ui.chat.composables.TopBar
import ai.deneb.ui.chat.composables.UserMessage
import ai.deneb.ui.chat.composables.WaitingResponseRow
import ai.deneb.ui.chat.composables.WorkReportBanner
import ai.deneb.ui.chat.composables.uiErrorText
import ai.deneb.ui.components.VerticalScrollbarForList
import ai.deneb.ui.components.generatingBackdrop
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebContentWidthModifier
import ai.deneb.ui.denebFadeEnter
import ai.deneb.ui.denebFadeExit
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.ChatbotTextScale
import ai.deneb.ui.markdown.precomputeMarkdownAsync
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.draganddrop.dragAndDropTarget
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.foundation.interaction.collectIsDraggedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.ime
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
import androidx.compose.ui.Alignment.Companion.TopStart
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
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.Dp
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
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.sin
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
    navigationTabBar: (@Composable () -> Unit)?,
) {
    // Hoisted here so the draft survives recompositions that remove QuestionInput
    // from composition and would otherwise drop the text.
    var questionInputText by rememberSaveable(stateSaver = TextFieldValue.Saver) {
        mutableStateOf(TextFieldValue(""))
    }
    val keyboardController = LocalSoftwareKeyboardController.current
    val snackbarHostState = remember { SnackbarHostState() }

    // Immersive top overlay: the top bar + banners float over the conversation,
    // which fills the full height and scrolls under them (and under the transparent
    // status bar — enableEdgeToEdge is set in MainActivity). The overlay's measured
    // height feeds the message list's top contentPadding so the first message rests
    // just below the bar instead of under it, while older messages scroll behind.
    val topOverlayDensity = LocalDensity.current
    var topOverlayHeightPx by remember { mutableStateOf(0) }
    // Same idea at the bottom: the input bar floats over the conversation; its
    // measured height becomes the list's bottom contentPadding so the last message
    // rests just above the input while older messages scroll behind it.
    var bottomOverlayHeightPx by remember { mutableStateOf(0) }

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

    // A failed send restores the user's text into the input so a typo or a long prompt
    // can be fixed and resent instead of retyped. Only the typed-send path sets
    // failedInput, and only when the box is empty so freshly typed text is never lost.
    LaunchedEffect(uiState.failedInput) {
        val failed = uiState.failedInput
        if (!failed.isNullOrBlank() && questionInputText.text.isBlank()) {
            questionInputText = TextFieldValue(failed, selection = TextRange(failed.length))
        }
    }

    val filteredConversations = remember(uiState.savedConversations, uiState.pendingConversationDeletion) {
        val pendingId = uiState.pendingConversationDeletion
        if (pendingId != null) uiState.savedConversations.filter { it.id != pendingId }.toImmutableList() else uiState.savedConversations
    }

    // The "generating" backdrop shows only during the thinking window — from send
    // until the answer's text starts rendering. True while loading and no non-empty
    // assistant answer sits after the latest user message yet; flips false (backdrop
    // fades to black) the moment the reply begins, matching the reference.
    val generatingActive = remember(uiState.history, uiState.isLoading) {
        if (!uiState.isLoading) {
            false
        } else {
            val lastUser = uiState.history.indexOfLast { it.role == History.Role.USER }
            val lastAnswer = uiState.history.indexOfLast {
                it.role == History.Role.ASSISTANT && !it.isThinking && it.content.isNotEmpty()
            }
            lastAnswer <= lastUser
        }
    }

    // 챗봇 workspace reads larger (font + line spacing); 업무 stays at 1f.
    val chatTextScale = if (uiState.recallEnabled) 1f else ChatbotTextScale

    // Swipe 챗봇 ↔ 업무 across the chat body (mirrors the top RecallModePill). The
    // gesture is keyed on Unit, so read the current mode + actions through
    // rememberUpdatedState rather than capturing a stale snapshot.
    val swipeHaptics = rememberHaptics()
    val recallNow = rememberUpdatedState(uiState.recallEnabled)
    val swipeActions = rememberUpdatedState(uiState.actions)

    // Soften the 챗봇 ↔ 업무 switch (swipe or top toggle): on a mode flip the chat
    // surface briefly fades down + slides in, masking the abrupt workspace/content
    // swap so it reads as a transition rather than a snap. Keyed off recallEnabled;
    // the prevRecall guard skips the animation on first composition (screen entry).
    val modeSwitchAnim = remember { Animatable(1f) }
    val prevRecall = remember { mutableStateOf(uiState.recallEnabled) }
    LaunchedEffect(uiState.recallEnabled) {
        if (prevRecall.value != uiState.recallEnabled) {
            prevRecall.value = uiState.recallEnabled
            modeSwitchAnim.snapTo(0f)
            modeSwitchAnim.animateTo(1f, tween(durationMillis = 320, easing = FastOutSlowInEasing))
        }
    }

    // Live follow-the-finger feedback for the 챗봇 ↔ 업무 swipe: the chat surface
    // tracks the drag (translates left, with resistance + a clamp) so the gesture
    // reads as "grabbing" the workspace, then springs back on release — replacing the
    // old discrete "drag past 72dp → instant snap" that felt unresponsive. On commit
    // the spring-back runs alongside modeSwitchAnim's fade/content-swap; on cancel it
    // just bounces back. Translation only (layer phase) — no per-frame recomposition.
    val swipeScope = rememberCoroutineScope()
    val swipeDragX = remember { Animatable(0f) }
    val swipeMaxTravelPx = with(LocalDensity.current) { 110.dp.toPx() }

    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Rtl) {
        // Vestigial outer drawer: the desktop product had a RIGHT session drawer here
        // (opened by a toolbar button). The native client is mobile-only now, so this is
        // inert — empty, gestures off, never opened. Kept as the layout wrapper so the
        // screen body below doesn't have to re-indent; sessions live in the LEFT drawer.
        ModalNavigationDrawer(
            drawerState = sessionDrawerState,
            gesturesEnabled = false,
            drawerContent = {},
        ) {
            CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                ModalNavigationDrawer(
                    drawerState = drawerState,
                    // The LEFT drawer is the session history (GPT/Claude-style), opened by
                    // the hamburger / left-edge swipe. Sections live on the bottom bar, so
                    // this drawer is sessions only.
                    drawerContent = {
                        DenebSessionDrawerSheet(
                            conversations = filteredConversations,
                            currentConversationId = uiState.currentConversationId,
                            pendingConversationDeletion = uiState.pendingConversationDeletion,
                            actions = uiState.actions,
                            onClose = { drawerScope.launch { drawerState.close() } },
                        )
                    },
                ) {
                    Box(
                        Modifier
                            .fillMaxSize()
                            .modeSwipeToggle(
                                onDrag = { dx ->
                                    // Right-to-left only (dx <= 0), damped + clamped so it
                                    // rubber-bands rather than tracking the finger 1:1.
                                    val resisted = (dx * 0.6f).coerceIn(-swipeMaxTravelPx, 0f)
                                    swipeScope.launch { swipeDragX.snapTo(resisted) }
                                },
                                onEnd = { committed ->
                                    swipeScope.launch {
                                        if (committed) {
                                            swipeHaptics.toggle(!recallNow.value)
                                            swipeActions.value.toggleRecall()
                                        }
                                        swipeDragX.animateTo(
                                            0f,
                                            spring(
                                                dampingRatio = Spring.DampingRatioNoBouncy,
                                                stiffness = Spring.StiffnessMediumLow,
                                            ),
                                        )
                                    }
                                },
                            )
                            .background(MaterialTheme.colorScheme.background)
                            // Gemini-style "generating" backdrop: a top-down hue-cycling glow
                            // behind everything while the reply is being thought up; fades to
                            // black once the answer starts rendering. Drawn over the solid
                            // background but under the content (top bar / chat / input).
                            // 챗봇·업무 공통 — 생성 중 오로라 글로우를 두 워크스페이스 모두에 표시.
                            .generatingBackdrop(active = generatingActive)
                            .navigationBarsPadding()
                            // No statusBarsPadding here: the conversation fills the full
                            // height and scrolls under the transparent status bar + the
                            // floating top overlay below (statusBarsPadding moves onto that
                            // overlay so its controls still clear the status bar).
                            .imePadding(),
                    ) {
                        Column(Modifier.fillMaxSize()) {
                            Box(
                                Modifier
                                    .weight(1f)
                                    // Mode-switch transition: a sinusoidal alpha dip on the
                                    // conversation ONLY (the top bar/pill + input stay put),
                                    // masking the async workspace/content swap. Layer phase,
                                    // so no per-frame recomposition.
                                    .graphicsLayer {
                                        val p = modeSwitchAnim.value
                                        alpha = 1f - sin(p * PI).toFloat() * 0.85f
                                        // Follow-the-finger offset during the 챗봇↔업무 swipe.
                                        translationX = swipeDragX.value
                                    },
                            ) {
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
                                                recallEnabled = uiState.recallEnabled,
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

                                        // Keyboard follow-scroll: when the soft keyboard opens, the floating
                                        // input bar rises with it (imePadding), shrinking the list viewport from
                                        // the bottom. Track the IME inset frame-by-frame and scroll the list by
                                        // the exact delta so the newest message rides up (and back down) glued to
                                        // the keyboard's own animation curve — smooth, not the stepped snaps a
                                        // bucketed scrollToItem gives. snapshotFlow keeps this on the effect
                                        // coroutine (no per-frame recomposition); near-bottom only, so a user
                                        // scrolled up to re-read isn't yanked.
                                        val imeInsets = WindowInsets.ime
                                        LaunchedEffect(listState, imeInsets) {
                                            var prev = imeInsets.getBottom(topOverlayDensity)
                                            snapshotFlow { imeInsets.getBottom(topOverlayDensity) }
                                                .collect { current ->
                                                    val delta = current - prev
                                                    prev = current
                                                    if (delta != 0 && isNearBottom) {
                                                        listState.scrollBy(delta.toFloat())
                                                    }
                                                }
                                        }

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
                                                                        textScale = chatTextScale,
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
                                                                        textScale = chatTextScale,
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
                                                                        textScale = chatTextScale,
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
                        }
                        // Immersive top overlay: the bar + banners float over the conversation
                        // (which scrolls full-height behind them, under the transparent status
                        // bar). The vertical scrim keeps the bar's controls legible over
                        // scrolling messages; statusBarsPadding clears the OS status bar; the
                        // measured height drives the message list's top contentPadding above.
                        Column(
                            modifier = Modifier
                                .align(TopStart)
                                .fillMaxWidth()
                                .onSizeChanged { topOverlayHeightPx = it.height }
                                .background(
                                    Brush.verticalGradient(
                                        0f to MaterialTheme.colorScheme.background,
                                        1f to Color.Transparent,
                                    ),
                                )
                                .statusBarsPadding(),
                        ) {
                            TopBar(
                                textToSpeech = textToSpeech,
                                isSpeechOutputEnabled = uiState.isSpeechOutputEnabled,
                                isSpeaking = uiState.isSpeaking,
                                actions = uiState.actions,
                                isChatHistoryEmpty = uiState.history.isEmpty(),
                                recallEnabled = uiState.recallEnabled,
                                // The hamburger opens the session history (left drawer).
                                onOpenDrawer = { drawerScope.launch { drawerState.open() } },
                                navigationTabBar = navigationTabBar,
                                // The desktop session button (right drawer) is gone — the
                                // hamburger above is the only way into sessions now.
                                onOpenSessionDrawer = null,
                            )

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
                        }
                        // Immersive bottom: the input bar floats over the conversation (which
                        // scrolls under it). A bottom-up scrim keeps it legible over scrolling
                        // messages; the measured height feeds the list's bottom contentPadding so
                        // the last message rests just above the input. The nav-bar + ime insets
                        // come from the root Box, so the input still sits above the gesture bar
                        // and rises with the keyboard. Same width cap as the message rows so it
                        // lines up with the conversation column on desktop.
                        Box(
                            modifier = Modifier
                                .align(BottomCenter)
                                .fillMaxWidth()
                                .onSizeChanged { bottomOverlayHeightPx = it.height }
                                .background(
                                    Brush.verticalGradient(
                                        0f to Color.Transparent,
                                        1f to MaterialTheme.colorScheme.background,
                                    ),
                                ),
                            contentAlignment = TopCenter,
                        ) {
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

// A right-to-left swipe across the chat body toggles 챗봇 ↔ 업무, mirroring the top
// RecallModePill. Only this one direction switches: a left-to-right swipe is left to
// the session drawer's open-swipe (left edge → right), so the two gestures never
// fight. Screen-edge starts are also yielded to the drawer, and a gesture that turns
// vertical is released so the message list still scrolls — only a clearly-horizontal
// right-to-left drag past the commit distance fires [onSwitch].
private fun Modifier.modeSwipeToggle(
    onDrag: (Float) -> Unit,
    onEnd: (committed: Boolean) -> Unit,
): Modifier = pointerInput(Unit) {
    val edge = 36.dp.toPx()
    val commit = 72.dp.toPx()
    val slop = viewConfiguration.touchSlop
    awaitEachGesture {
        val down = awaitFirstDown(requireUnconsumed = false)
        if (down.position.x <= edge || down.position.x >= size.width - edge) {
            return@awaitEachGesture // edge zone: leave it to the drawer open-swipe
        }
        var dx = 0f
        var dy = 0f
        var horizontal = false
        while (true) {
            val change = awaitPointerEvent().changes.firstOrNull { it.id == down.id } ?: break
            if (!change.pressed) break
            val delta = change.positionChange()
            dx += delta.x
            dy += delta.y
            if (!horizontal) {
                if (abs(dy) > slop && abs(dy) >= abs(dx)) return@awaitEachGesture // vertical → scroll
                if (abs(dx) > slop && abs(dx) > abs(dy)) horizontal = true
            }
            if (horizontal) {
                change.consume()
                onDrag(dx) // live offset; the composable clamps to right-to-left + resists
            }
        }
        // Commit only a clearly-horizontal right-to-left drag past the distance; onEnd
        // always fires (even on cancel) so the live offset can spring back.
        onEnd(horizontal && dx <= -commit)
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
