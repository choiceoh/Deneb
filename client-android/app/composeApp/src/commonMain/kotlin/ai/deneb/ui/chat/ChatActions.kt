package ai.deneb.ui.chat

import androidx.compose.runtime.Immutable
import io.github.vinceglb.filekit.PlatformFile

@Immutable
data class ChatActions(
    val ask: (String) -> Unit,
    val toggleSpeechOutput: () -> Unit,
    // Toggles the gateway long-term-memory recall (focused chat / memory off).
    val toggleRecall: () -> Unit,
    val retry: () -> Unit,
    val clearHistory: () -> Unit,
    val setIsSpeaking: (Boolean, String) -> Unit,
    val addFile: (PlatformFile) -> Unit,
    val removeFile: (PlatformFile) -> Unit,
    val startNewChat: () -> Unit,
    val regenerate: () -> Unit,
    val cancel: () -> Unit,
    val selectService: (String) -> Unit,
    val loadConversation: (String) -> Unit,
    val deleteConversation: (String) -> Unit,
    val clearUnreadHeartbeat: () -> Unit,
    val clearUnreadWorkReport: () -> Unit,
    val openWorkReport: () -> Unit,
    val openWorkFeedItem: (String) -> Unit,
    val refreshWorkFeedRange: (Long, Long) -> Unit,
    // Clears ChatUiState.pendingScrollToMessageId after the chat list lands on it.
    val consumePendingScroll: () -> Unit,
    val runWorkFeedAction: (String, String) -> Unit,
    // Answer a question card inline: a choice chip (actionId set) runs that action;
    // a free-text reply (actionId null) sends the answer to the card's session. Both
    // route the answer to the asking agent and settle the card. (item, answer, actionId?)
    val answerWorkFeed: (WorkFeedItem, String, String?) -> Unit,
    // Long-press a feed card → 정정·피드백: teach/correct the agent. (itemId, feedback)
    val submitWorkFeedFeedback: (String, String) -> Unit,
    // Long-press a feed card → 다시 작성: regenerate the card's analysis in place. (itemId)
    val rewriteWorkFeedCard: (String) -> Unit,
    val clearSnackbar: () -> Unit,
    // Clears ChatUiState.feedbackResultText after the feed shows the agent's report.
    val clearFeedbackResult: () -> Unit,
    val undoDeleteConversation: () -> Unit,
    val submitUiCallback: (event: String, data: Map<String, String>) -> Unit,
    val resubmit: (messageId: String, event: String, data: Map<String, String>) -> Unit,
    val sendSmsDraft: (String) -> Unit,
    val discardSmsDraft: (String) -> Unit,
    // Reload the session list from the gateway — fired when the drawer opens so it
    // never shows a stale list (the list is otherwise only loaded once at startup).
    val refreshConversations: () -> Unit,
)
