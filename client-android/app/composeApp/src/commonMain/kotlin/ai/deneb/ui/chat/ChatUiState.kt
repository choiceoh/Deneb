@file:OptIn(ExperimentalUuidApi::class)

package ai.deneb.ui.chat

import ai.deneb.data.Attachment
import ai.deneb.data.FallbackStatus
import ai.deneb.data.ServiceEntry
import ai.deneb.data.SmsDraft
import ai.deneb.data.UiSubmission
import ai.deneb.network.UiError
import androidx.compose.runtime.Immutable
import io.github.vinceglb.filekit.PlatformFile
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.serialization.Serializable
import org.jetbrains.compose.resources.StringResource
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

@Immutable
data class ConversationSummary(
    val id: String,
    val title: String,
    val updatedAt: Long,
    val isHeartbeat: Boolean = false,
    val model: String = "",
    val pinned: Boolean = false,
    val snippet: String = "",
)

@Immutable
@Serializable
data class WorkFeedAction(
    val id: String = "",
    val kind: String = "",
    val label: String = "",
    val status: String = "",
    val prompt: String = "",
)

@Immutable
@Serializable
data class WorkFeedItem(
    val id: String = "",
    val source: String = "",
    val title: String = "",
    val summary: String = "",
    val body: String = "",
    val sessionKey: String = "",
    // Advisory semantic grouping from the gateway. Related cards remain
    // independent feed entries; these fields only explain their relationship.
    val clusterId: String = "",
    val relatedIds: List<String> = emptyList(),
    val status: String = "",
    val priority: Int = 0,
    val actions: List<WorkFeedAction> = emptyList(),
    // Question = the agent is asking the user to answer (a deal-team question, or a
    // proactive turn that posed a question / offered choices). The feed renders an
    // inline answer affordance: the actions as chips, or a free-text reply field.
    val question: Boolean = false,
    val createdAtMs: Long = 0,
    // Set once the card is opened (read) on any device; 0 = unread. Softer than the
    // server ack — the card stays in the feed, just de-emphasized. Shared via the
    // gateway so reading on the desktop shows read here too.
    val readAtMs: Long = 0,
    // Optional durable pointer (e.g. Amaranth docId for groupware-approval chips).
    val refId: String = "",
)

/**
 * Proactive reports (mail analyses, morning letters) are mirrored into the
 * client:main 업무 transcript by the gateway relay; opening their card jumps
 * to that mirror. Capture cards have no mirror and keep the dedicated
 * side-conversation open path.
 */
val WorkFeedItem.isProactiveReport: Boolean
    get() = source == "proactive"

/**
 * System/agent cards belong on the 로그 pivot, not the 업무 피드.
 * Source only — keep aligned with `workfeed.IsLogSource` in gateway-go.
 * The gateway stamps `system_log` / `genesis-*` at write time (and rewrites
 * older proactive diagnostic cards on read).
 */
fun isWorkFeedLogCard(source: String): Boolean {
    val s = source.trim()
    return s == "system_log" || s == "genesis-meta" || s == "genesis-ladder" || s.startsWith("genesis-")
}

val WorkFeedItem.isLogCard: Boolean
    get() = isWorkFeedLogCard(source)

/**
 * One message queued while a reply was still streaming ([ChatUiState.pendingQuestions]).
 *
 * [restoreToInput] separates the two producers: `true` for user-typed sends (safe to
 * surface back into the input box when a turn fails / stops / the session switches),
 * `false` for programmatic prompts (work-feed card actions, UI callbacks) whose text
 * must never appear verbatim in the input box — those are dropped instead of restored.
 * [files] snapshots the attachments staged when the message was queued, so they ride
 * with THIS message instead of being absorbed by whichever send happens first.
 */
@Immutable
data class PendingQuestion(
    val text: String,
    val restoreToInput: Boolean,
    val files: ImmutableList<PlatformFile> = persistentListOf(),
)

@Immutable
data class ChatUiState(
    val actions: ChatActions,
    val history: ImmutableList<History> = persistentListOf(),
    val isSpeechOutputEnabled: Boolean = false,
    val isLoading: Boolean = false,
    val error: UiError? = null,
    val warning: StringResource? = null,
    val supportedFileExtensions: ImmutableList<String> = persistentListOf(),
    val isSpeaking: Boolean = false,
    val isSpeakingContentId: String = "",
    val files: ImmutableList<PlatformFile> = persistentListOf(),
    val availableServices: ImmutableList<ServiceEntry> = persistentListOf(),
    val savedConversations: ImmutableList<ConversationSummary> = persistentListOf(),
    // The gateway holds conversations this drawer has not pulled in yet.
    val hasMoreConversations: Boolean = false,
    val currentConversationId: String? = null,
    val hasUnreadHeartbeat: Boolean = false,
    val hasUnreadWorkReport: Boolean = false,
    val workFeed: ImmutableList<WorkFeedItem> = persistentListOf(),
    // False until the feed's first fetch finishes, so the 피드 home shows a loading
    // skeleton instead of flashing "오늘 받은 피드가 없습니다" on cold launch.
    val workFeedLoaded: Boolean = false,
    // One-shot scroll target: set when opening a proactive work-feed card jumps
    // into client:main, consumed by the chat list once the message is visible.
    val pendingScrollToMessageId: String? = null,
    // Previous versions of the LAST answer, stashed when 다시 생성 replaces it —
    // navigable ‹ n/N ›. Live-session only (a reload rebuilds from the transcript,
    // which appends regenerated turns sequentially instead). Cleared on any new
    // question / session switch. lastAnswerVariantIndex ∈ [0, variants.size]:
    // variants.size selects the live (newest) answer.
    val lastAnswerVariants: ImmutableList<History> = persistentListOf(),
    val lastAnswerVariantIndex: Int = 0,
    val smsDrafts: ImmutableList<SmsDraft> = persistentListOf(),
    val snackbarMessage: StringResource? = null,
    // Free-text agent report from a feed-card feedback turn (gateway returns "what I
    // updated in the wiki"); shown as a snackbar on the feed, then cleared.
    val feedbackResultText: String? = null,
    val pendingConversationDeletion: String? = null,
    val sessionSearchHits: ImmutableList<ConversationSummary> = persistentListOf(),
    // Messages sent while a reply was still streaming: queued client-side (FIFO)
    // and auto-sent the moment the running turn completes SUCCESSFULLY. An errored
    // or stopped turn never auto-sends — user-typed entries fold back into
    // failedInput so the user can rephrase in light of what happened; programmatic
    // entries are dropped. A session switch / new chat also clears the queue (a
    // queued message must never fire into a different conversation).
    val pendingQuestions: ImmutableList<PendingQuestion> = persistentListOf(),
    // Latest mid-turn steer the gateway accepted while this reply streams.
    // Cleared when the turn ends (success, failure, stop, or session switch).
    val lastSteerNote: String? = null,
    val fallbackStatus: FallbackStatus? = null,
    val isRestoring: Boolean = true,
    // The user-typed message whose send failed, surfaced back into the input so a
    // typo / long prompt can be fixed instead of retyped. Only the ask() path sets it.
    val failedInput: String? = null,
    // Id of the assistant message whose streaming the user stopped, so the UI marks
    // it 중단됨 instead of leaving a half-answer that looks complete.
    val stoppedMessageId: String? = null,
    // The user stopped before any answer text arrived, so there is no assistant row
    // to mark. Distinct from [stoppedMessageId] because the transcript shape it
    // leaves — a user turn with no answer — is identical to a silently dropped
    // reply, and offering "다시 생성" for something the user just cancelled reads
    // as a failure the app is asking them to retry.
    val stoppedBeforeAnswer: Boolean = false,
) {
    val heartbeatConversationId: String?
        get() = savedConversations.firstOrNull { it.isHeartbeat }?.id
}

@Immutable
data class History(
    val id: String = Uuid.random().toString(),
    val role: Role,
    val content: String,
    val attachments: ImmutableList<Attachment> = persistentListOf(),
    val toolCallId: String? = null,
    val toolName: String? = null,
    val toolCalls: ImmutableList<ToolCallInfo>? = null,
    val isThinking: Boolean = false,
    val isStatusMessage: Boolean = false,
    val fallbackServiceName: String? = null,
    // Compact trail of the tools this answer's turn ran ("메일 확인 ×2 · 웹 검색"),
    // shown as a meta line under the bubble. Filled live by TurnProgress and on
    // transcript reload from the server's toolTrace — same builder, same format.
    val toolFootprint: String? = null,
    val uiSubmission: UiSubmission? = null,
    // Preserved from a tool-call assistant turn so it can be round-tripped
    // back to providers (e.g. DeepSeek) that require it on the next request.
    val reasoningContent: String? = null,
    // Wall-clock of the message as stored in the gateway transcript; 0 when
    // unknown (live streaming rows, non-gateway repositories). Lets a proactive
    // work-feed card locate its mirrored transcript message — both are stamped
    // within the same relay call on the gateway.
    val timestampMs: Long = 0,
) {
    enum class Role {
        USER,
        ASSISTANT,
        TOOL_EXECUTING,
        TOOL,
    }
}

/**
 * Stable identity for a transcript-sourced message. Both the local cache decode
 * and the network fetch rebuild [History] from role + content + timestamp, so a
 * default random UUID would give the SAME message a NEW id on every reload — and
 * the message list keys the LazyColumn on `id` with `animateItem`, so a re-key
 * makes the old and new rows animate as separate items and briefly OVERLAP
 * (the "같은 채팅이 두 번 렌더링돼 겹침" bug). Deriving the id from the persisted
 * fields keeps it identical across cache/network/change-feed reloads, so the row
 * is reused instead of re-inserted. Live optimistic rows keep their random UUID
 * (they carry no stable timestamp yet) and settle to this id on the next reload.
 */
fun stableTranscriptId(role: History.Role, content: String, timestampMs: Long): String = "tx:${role.name}:$timestampMs:${content.hashCode()}"

/** Latest assistant message that should render in the UI (non-empty content, not a thinking-only entry). */
fun List<History>.lastRenderedAssistant(): History? = lastOrNull { it.role == History.Role.ASSISTANT && it.content.isNotEmpty() && !it.isThinking }

/** True when the latest user turn has no answer-bearing assistant after it. */
fun List<History>.hasUnansweredUserTurn(): Boolean {
    val lastUser = indexOfLast { it.role == History.Role.USER }
    if (lastUser < 0) return false
    val lastAnswer = indexOfLast {
        it.role == History.Role.ASSISTANT && !it.isThinking && it.content.isNotEmpty()
    }
    return lastAnswer < lastUser
}

/** [hasUnansweredUserTurn] minus live narration: an executing/status row (the
 * foreign-turn watch, a recovery poll) means the turn is NOT silently dead —
 * offering 다시 생성 beside it would double-send into the running turn. */
fun List<History>.silentlyUnanswered(): Boolean = none { it.role == History.Role.TOOL_EXECUTING } && hasUnansweredUserTurn()

/**
 * Silent-death recovery: the turn finished, nothing rendered, no error banner —
 * and the user did not stop it themselves.
 */
fun ChatUiState.needsEmptyReplyRecovery(): Boolean = !isLoading && !isRestoring && error == null && !stoppedBeforeAnswer && history.silentlyUnanswered()

@Immutable
data class ToolCallInfo(
    val id: String,
    val name: String,
    val arguments: String,
    val thoughtSignature: String? = null,
)
