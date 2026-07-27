@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat

import ai.deneb.ui.chat.composables.ChatInputOverlay
import ai.deneb.ui.chat.composables.ChatMessageList
import ai.deneb.ui.chat.composables.ChatTopOverlay
import ai.deneb.ui.chat.composables.DenebSessionDrawerSheet
import ai.deneb.ui.components.generatingBackdrop
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment.Companion.BottomCenter
import androidx.compose.ui.Alignment.Companion.TopStart
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.launch
import nl.marc_apps.tts.TextToSpeechInstance
import org.jetbrains.compose.resources.getString

/**
 * Regular chat mode of the chat surface: the message list, input chrome, banners,
 * and the executing-tools indicator. Split out of ChatScreen.kt so the mode can
 * grow without re-bloating the entry file. The list, top overlay, and input
 * overlay each live in their own file under composables/.
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

    // The chat stays composed while another tab is selected (LiveTabPane) — a
    // hidden tab must never intercept system back, so both drawer handlers AND
    // with the active flag.
    val tabActive = ai.deneb.ui.LocalLiveTabActive.current

    // Left navigation drawer (analysis surfaces): opened by the top-bar
    // hamburger or a left-edge swipe; system back closes it before exiting.
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    val drawerScope = rememberCoroutineScope()
    ai.deneb.PlatformBackHandler(enabled = tabActive && drawerState.isOpen) {
        drawerScope.launch { drawerState.close() }
    }

    // Right-side session selector: opened by the top-bar session button or a
    // right-edge swipe (mirroring the left drawer); dismissed by scrim or back.
    val sessionDrawerState = rememberDrawerState(DrawerValue.Closed)
    ai.deneb.PlatformBackHandler(enabled = tabActive && sessionDrawerState.isOpen) {
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
    // can be fixed and resent instead of retyped. Only the typed-send path (and the
    // queue folds — error/stop/session switch) sets failedInput. An empty box is
    // filled; a non-empty box gets the restored text APPENDED after the draft —
    // this effect keys on failedInput only and never re-fires when the box later
    // empties, so skipping a non-empty box would silently lose the text (queued
    // messages folded while the user was typing a follow-up).
    LaunchedEffect(uiState.failedInput) {
        val failed = uiState.failedInput
        if (failed.isNullOrBlank()) return@LaunchedEffect
        val draft = questionInputText.text
        if (draft == failed) return@LaunchedEffect // box already holds exactly this restore
        val merged = if (draft.isBlank()) failed else "$draft\n\n$failed"
        questionInputText = TextFieldValue(merged, selection = TextRange(merged.length))
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
                            hasMoreConversations = uiState.hasMoreConversations,
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
                            .background(MaterialTheme.colorScheme.background)
                            // Gemini-style "generating" backdrop: a top-down hue-cycling glow
                            // behind everything while the reply is being thought up; fades to
                            // black once the answer starts rendering. Drawn over the solid
                            // background but under the content (top bar / chat / input).
                            .generatingBackdrop(active = generatingActive)
                            .navigationBarsPadding()
                            // No statusBarsPadding here: the conversation fills the full
                            // height and scrolls under the transparent status bar + the
                            // floating top overlay below (statusBarsPadding moves onto
                            // that overlay so its controls still clear the status bar).
                            // imePadding on the root Box lifts BOTH the input bar and
                            // the list above the keyboard. The list's follow-scroll
                            // (see ChatMessageList) then rides the newest message up so
                            // it rests exactly above the input bar as the keyboard opens.
                            .imePadding(),
                    ) {
                        Column(Modifier.fillMaxSize()) {
                            ChatMessageList(
                                uiState = uiState,
                                textToSpeech = textToSpeech,
                                topOverlayDensity = topOverlayDensity,
                                topOverlayHeightPx = topOverlayHeightPx,
                                bottomOverlayHeightPx = bottomOverlayHeightPx,
                                modifier = Modifier.weight(1f),
                            )
                        }

                        ChatTopOverlay(
                            uiState = uiState,
                            textToSpeech = textToSpeech,
                            navigationTabBar = navigationTabBar,
                            onOpenDrawer = { drawerScope.launch { drawerState.open() } },
                            modifier = Modifier.align(TopStart),
                            onHeightChange = { topOverlayHeightPx = it },
                        )

                        ChatInputOverlay(
                            uiState = uiState,
                            questionInputText = questionInputText,
                            onQuestionInputTextChange = { questionInputText = it },
                            modifier = Modifier.align(BottomCenter),
                            onHeightChange = { bottomOverlayHeightPx = it },
                        )

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
