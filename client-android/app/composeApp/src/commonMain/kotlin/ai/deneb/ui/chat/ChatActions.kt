package ai.deneb.ui.chat

import androidx.compose.runtime.Immutable
import io.github.vinceglb.filekit.PlatformFile

@Immutable
data class ChatActions(
    val ask: (String) -> Unit,
    val toggleSpeechOutput: () -> Unit,
    val retry: () -> Unit,
    val clearHistory: () -> Unit,
    val setIsSpeaking: (Boolean, String) -> Unit,
    val addFile: (PlatformFile) -> Unit,
    val removeFile: (PlatformFile) -> Unit,
    val startNewChat: () -> Unit,
    val regenerate: () -> Unit,
    // 마지막 사용자 메시지를 고쳐 다시 보낸다 — regenerate와 같은 시맨틱(마지막 교환을
    // 로컬에서 되감고 재질의)에 텍스트만 바뀐다. 마지막 메시지 한정: 그 이전을 편집하면
    // 이후 대화가 서버 컨텍스트에 남아 정직하지 않다 (transcript truncation RPC 부재).
    val editResendLast: (String) -> Unit,
    // ‹ n/N › — 다시 생성으로 대체된 이전 답변들 사이를 오간다 (ChatUiState.lastAnswerVariants).
    val selectAnswerVariant: (Int) -> Unit,
    val cancel: () -> Unit,
    // Drops the messages queued while a reply was streaming (ChatUiState.pendingQuestions).
    val cancelPendingQuestions: () -> Unit,
    val selectService: (String) -> Unit,
    val loadConversation: (String) -> Unit,
    val deleteConversation: (String) -> Unit,
    val renameConversation: (String, String) -> Unit,
    val clearUnreadHeartbeat: () -> Unit,
    val clearUnreadWorkReport: () -> Unit,
    val openWorkReport: () -> Unit,
    val openWorkFeedItem: (String) -> Unit,
    // Stamp a feed card read on the gateway (durable + cross-device) when it's opened.
    val markWorkFeedRead: (String) -> Unit,
    // Suspends and reports success so the feed's pull-to-refresh spinner and
    // failure banner track the real fetch instead of a fixed timer.
    val refreshWorkFeedRange: suspend (Long, Long) -> Boolean,
    // Clears ChatUiState.pendingScrollToMessageId after the chat list lands on it.
    val consumePendingScroll: () -> Unit,
    val runWorkFeedAction: (String, String) -> Unit,
    // Answer a question card inline: a choice chip (actionId set) runs that action;
    // a free-text reply (actionId null) sends the answer to the card's session. Both
    // route the answer to the asking agent and settle the card. Approval rejection
    // can carry a transient comment. (item, answer, actionId?, comment?)
    val answerWorkFeed: (WorkFeedItem, String, String?, String?) -> Unit,
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
    // Appends the next page of older conversations to the drawer list.
    val loadMoreConversations: () -> Unit,
)
