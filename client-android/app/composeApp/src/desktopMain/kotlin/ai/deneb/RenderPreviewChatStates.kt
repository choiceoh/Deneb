package ai.deneb

import ai.deneb.data.ServiceEntry
import ai.deneb.data.SmsDraft
import ai.deneb.data.SmsDraftStatus
import ai.deneb.network.UiError
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.chat.ChatActions
import ai.deneb.ui.chat.ConversationSummary
import ai.deneb.ui.chat.composables.DenebSessionDrawerSheet
import ai.deneb.ui.chat.composables.ErrorMessage
import ai.deneb.ui.chat.composables.HeartbeatBanner
import ai.deneb.ui.chat.composables.PendingSmsBanners
import ai.deneb.ui.chat.composables.ServiceSelector
import ai.deneb.ui.chat.composables.WorkReportBanner
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_service_anthropic
import kotlinx.collections.immutable.persistentListOf

/**
 * Fixtures for the chat's TRANSIENT-STATE components — error banner, heartbeat /
 * work-report banners, staged SMS drafts, and the service picker.
 *
 * These only appear when something specific happens (a request fails, a heartbeat
 * lands, the agent stages an SMS), so the live harness cannot
 * make them show on demand. That made them the blind spot of the DenebType sweep:
 * compilation proved they still built, and nothing proved they still looked right.
 */

private val previewSessions = persistentListOf(
    ConversationSummary(
        id = "client:main",
        title = "업무",
        updatedAt = 1_787_500_000_000,
    ),
    ConversationSummary(
        id = "client:main:nda",
        title = "아르고에너지 NDA 조항 검토",
        updatedAt = 1_787_400_000_000,
        pinned = true,
        snippet = "손해배상 상한과 EPC 관례",
    ),
    ConversationSummary(
        id = "client:main:letter",
        title = "모닝레터 후속",
        updatedAt = 1_787_300_000_000,
        model = "anthropic/claude-opus-4",
    ),
)

@Composable
internal fun sessionDrawerBody(
    scheme: ColorScheme,
    searchOpen: Boolean = false,
    revealedId: String? = null,
) {
    MaterialTheme(colorScheme = scheme) {
        Surface(
            color = MaterialTheme.colorScheme.background,
            modifier = Modifier.width(360.dp).height(720.dp),
        ) {
            DenebSessionDrawerSheet(
                conversations = previewSessions,
                hasMoreConversations = true,
                currentConversationId = "client:main:nda",
                pendingConversationDeletion = null,
                actions = previewChatActions(),
                onClose = {},
                initiallySearchOpen = searchOpen,
                initiallyRevealedSessionId = revealedId,
            )
        }
    }
}

/**
 * A ChatActions whose every callback is a no-op. [ChatUiState] requires one and it
 * has 35 members with no defaults — which is precisely why chat-list previews did
 * not exist before. Kept here so any future chat preview constructs state in one
 * line instead of re-deriving this wall.
 */
internal fun previewChatActions(): ChatActions = ChatActions(
    ask = {},
    toggleSpeechOutput = {},
    retry = {},
    clearHistory = {},
    setIsSpeaking = { _, _ -> },
    addFile = {},
    removeFile = {},
    startNewChat = {},
    regenerate = {},
    editResendLast = {},
    selectAnswerVariant = {},
    cancel = {},
    cancelPendingQuestions = {},
    selectService = {},
    loadConversation = {},
    deleteConversation = {},
    renameConversation = { _, _ -> },
    pinConversation = { _, _ -> },
    resetConversationModel = {},
    searchConversations = {},
    clearUnreadHeartbeat = {},
    clearUnreadWorkReport = {},
    openWorkReport = {},
    openWorkFeedItem = {},
    markWorkFeedRead = {},
    refreshWorkFeedRange = { _, _ -> true },
    consumePendingScroll = {},
    runWorkFeedAction = { _, _ -> },
    answerWorkFeed = { _, _, _, _ -> },
    submitWorkFeedFeedback = { _, _ -> },
    rewriteWorkFeedCard = {},
    clearSnackbar = {},
    clearFeedbackResult = {},
    undoDeleteConversation = {},
    submitUiCallback = { _, _ -> },
    resubmit = { _, _, _ -> },
    sendSmsDraft = {},
    discardSmsDraft = {},
    refreshConversations = {},
    loadMoreConversations = {},
)

private fun previewSmsDraft(id: String, status: SmsDraftStatus, error: String? = null) = SmsDraft(
    id = id,
    address = "010-1234-5678",
    body = "내일 오후 3시 곡성공장 현장 미팅 확인 부탁드립니다. 자재 반입 일정도 같이 정리해서 가겠습니다.",
    createdAtEpochMs = 1_784_000_000_000,
    status = status,
    lastError = error,
)

/**
 * Every transient state stacked under its own section label, so a regression is
 * attributable at a glance. All four SMS statuses are present because each renders
 * different trailing content, and FAILED is the only one that must also fit an
 * error string.
 */
@Composable
internal fun chatStatesBody(scheme: ColorScheme) {
    MaterialTheme(colorScheme = scheme) {
        Surface(color = MaterialTheme.colorScheme.background) {
            Column(Modifier.width(412.dp)) {
                DenebSectionLabel("오류", Modifier.padding(start = 24.dp, top = 16.dp))
                ErrorMessage(UiError.Text("게이트웨이에 연결하지 못했습니다 (timeout after 30s)"), onDismiss = {})

                DenebSectionLabel("하트비트 배너", Modifier.padding(start = 24.dp, top = 8.dp))
                HeartbeatBanner(visible = true, onTap = {}, onDismiss = {})

                DenebSectionLabel("업무보고 배너", Modifier.padding(start = 24.dp, top = 8.dp))
                WorkReportBanner(visible = true, onTap = {}, onDismiss = {})

                DenebSectionLabel("서비스 선택", Modifier.padding(start = 24.dp, top = 8.dp))
                ServiceSelector(
                    services = persistentListOf(
                        ServiceEntry("i1", "anthropic", "Anthropic", "claude-opus-5", Res.drawable.ic_service_anthropic),
                        ServiceEntry("i2", "moonshot", "Moonshot", "kimi-k3", Res.drawable.ic_service_anthropic),
                    ),
                    onSelectService = {},
                )

                DenebSectionLabel("SMS 초안", Modifier.padding(start = 24.dp, top = 8.dp))
                PendingSmsBanners(
                    drafts = persistentListOf(
                        previewSmsDraft("d1", SmsDraftStatus.PENDING),
                        previewSmsDraft("d2", SmsDraftStatus.SENDING),
                        previewSmsDraft("d3", SmsDraftStatus.SENT),
                        previewSmsDraft("d4", SmsDraftStatus.FAILED, "SIM not ready"),
                    ),
                    onSend = {},
                    onDiscard = {},
                )
            }
        }
    }
}

/*
 * NOT PREVIEWED: the per-answer meta annotations (중단됨 · 도구 발자국 · 폴백) live
 * inside ChatMessageList, and that list cannot render in this harness — its
 * auto-scroll effect calls performMeasureAndLayout from a LaunchedEffect, which
 * throws under ImageComposeScene's single-frame render and takes the whole
 * renderPreviews run down with it (measured: main() died and every preview after
 * it stopped being written). The alternative is a preview-only flag threaded
 * through production scroll code — a worse trade than leaving three Text nodes
 * uncovered. previewChatActions() above stays, ready for the day the list gets a
 * testable seam.
 */
