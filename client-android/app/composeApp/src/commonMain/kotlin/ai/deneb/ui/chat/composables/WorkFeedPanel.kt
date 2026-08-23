package ai.deneb.ui.chat.composables

import ai.deneb.deneb.DenebEmpty
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebPressable
import ai.deneb.ui.handCursor
import ai.deneb.ui.icons.filled.Mic
import ai.deneb.ui.icons.filled.PushPin
import ai.deneb.ui.icons.filled.School
import ai.deneb.ui.icons.filled.TaskAlt
import ai.deneb.ui.icons.filled.Terminal
import ai.deneb.ui.icons.filled.Verified
import ai.deneb.ui.icons.outlined.Archive
import ai.deneb.ui.icons.outlined.Article
import ai.deneb.ui.icons.outlined.AutoAwesome
import ai.deneb.ui.icons.outlined.Autorenew
import ai.deneb.ui.icons.outlined.Bolt
import ai.deneb.ui.icons.outlined.Book
import ai.deneb.ui.icons.outlined.KeyboardVoice
import ai.deneb.ui.icons.outlined.QuestionAnswer
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.MailOutline
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_file
import deneb.composeapp.generated.resources.ic_image
import deneb.composeapp.generated.resources.work_feed_title
import kotlinx.collections.immutable.ImmutableList
import kotlinx.coroutines.delay
import org.jetbrains.compose.resources.painterResource
import org.jetbrains.compose.resources.stringResource
import kotlin.time.Clock

internal const val MaxApprovalCommentCharacters = 500

private val InlineButtonEventRegex = Regex(
    pattern = """<button\b[^>]*\bevent\s*=\s*[\"']([^\"']+)[\"']""",
    option = RegexOption.IGNORE_CASE,
)

internal fun approvalActionIdForUiEvent(event: String): String? {
    val normalized = event.trim().lowercase()
    return when {
        normalized == "approval:approve" || normalized == "approve" || normalized.startsWith("approve_") ->
            "approval:approve"

        normalized == "approval:reject" || normalized == "reject" || normalized.startsWith("reject_") ->
            "approval:reject"

        else -> null
    }
}

internal fun WorkFeedItem.hasInlineApprovalActions(): Boolean {
    if (source != "groupware-approval") return false
    val availableActionIds = actions.mapTo(mutableSetOf()) { it.id }
    val inlineActionIds = InlineButtonEventRegex.findAll(body)
        .mapNotNull { match -> approvalActionIdForUiEvent(match.groupValues[1]) }
        .toSet()
    return setOf("approval:approve", "approval:reject").all { actionId ->
        actionId in availableActionIds && actionId in inlineActionIds
    }
}

internal fun approvalCommentCharacterCount(value: String): Int {
    var count = 0
    var index = 0
    while (index < value.length) {
        val char = value[index]
        index += if (char.isHighSurrogate() && index + 1 < value.length && value[index + 1].isLowSurrogate()) 2 else 1
        count++
    }
    return count
}

internal fun limitApprovalComment(value: String): String {
    var count = 0
    var end = 0
    while (end < value.length && count < MaxApprovalCommentCharacters) {
        val char = value[end]
        end += if (char.isHighSurrogate() && end + 1 < value.length && value[end + 1].isLowSurrogate()) 2 else 1
        count++
    }
    return value.substring(0, end)
}

/**
 * Urgent = the gateway's top priority band (workfeed.PriorityUrgent = 4), the one
 * that outranks recency in the feed's sort order. Kept as a named predicate so the
 * marker and any future urgent-only affordance agree on one threshold instead of
 * scattering a magic 4.
 */
internal fun WorkFeedItem.isUrgent(): Boolean = priority >= WORK_FEED_PRIORITY_URGENT

internal const val WORK_FEED_PRIORITY_URGENT = 4

/**
 * Bottom-sheet content for the work feed (action inbox), in the Deneb idiom:
 * typography on a flat surface (no card), [DenebRow] hairlines instead of
 * dividers, and Deneb type roles instead of Material's. Each row leads with a
 * source icon (mail report / image / audio / contacts), an unread item gets the
 * strong title weight, and the quick actions are a compact trailing icon row.
 * Lists every item in a scrollable LazyColumn (no cap); shows an empty state
 * when nothing is pending.
 */
@Composable
internal fun WorkFeedPanel(
    items: ImmutableList<WorkFeedItem>,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
    onClose: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .navigationBarsPadding(),
    ) {
        // Header: Deneb subject title + pending count; close stays a Material control.
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 20.dp, top = 10.dp, end = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(Res.string.work_feed_title),
                style = DenebType.subject,
                color = MaterialTheme.colorScheme.onBackground,
            )
            if (items.isNotEmpty()) {
                Text(
                    text = items.size.toString(),
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
            Spacer(Modifier.weight(1f))
            IconButton(modifier = Modifier.handCursor(), onClick = onClose) {
                Icon(
                    imageVector = Icons.Filled.Close,
                    contentDescription = "닫기",
                    tint = denebHint(),
                )
            }
        }
        if (items.isEmpty()) {
            DenebEmpty("아직 업무 알림이 없습니다")
        } else {
            // Cap the height so a long feed scrolls inside the sheet instead of
            // pushing the sheet past the screen.
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 520.dp),
            ) {
                // The gateway deliberately tolerates duplicate item ids (ack/
                // trash/snooze apply to EVERY entry sharing the id — the
                // zombie-duplicate safety net), and a duplicate LazyColumn key
                // crashes. The id#position composite is unique by construction
                // and stays stable for this rebuilt-on-refresh list.
                itemsIndexed(items, key = { index, item -> "${item.id}#$index" }) { _, item ->
                    WorkFeedRow(item = item, onOpen = onOpen, onRunAction = onRunAction)
                }
            }
        }
    }
}

@Composable
internal fun WorkFeedRow(
    item: WorkFeedItem,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
    expanded: Boolean = false,
    onLongAction: ((WorkFeedItem) -> Unit)? = null,
) {
    // The row already leads with a source icon, so a "📬 …" title would show two
    // icons side by side — strip the leading emoji/symbol run from the title.
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else stripLeadingIcon(item.title)
    val haptics = rememberHaptics()
    val titleStyle = if (item.status == "unread") DenebType.rowTitleStrong else DenebType.rowTitle
    DenebRow(
        onClick = {
            haptics.tap()
            onOpen(item.id)
        },
        // No manual longPress() here — combinedClickable under DenebRow already
        // fires the long-press haptic (foundation 1.9+); doubling reads as a stutter.
        onLongClick = onLongAction?.let { { it(item) } },
        modifier = Modifier.padding(horizontal = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            val source = workFeedSourcePresentation(item.source)
            // Urgency rides the SOURCE ICON's tint rather than a dot on the title
            // line. The dot used to sit in the text flow, so an urgent card's title
            // started 12.5dp right of its own summary while a routine card's sat
            // flush — the feed's left edge alternated card by card. The icon column
            // is already a fixed gutter, so marking it there costs no width and
            // keeps the signal leading, where it gets scanned first.
            Icon(
                painter = sourcePainter(source.icon),
                contentDescription = if (item.isUrgent()) "긴급 · ${source.label}" else source.label,
                tint = if (item.isUrgent()) denebInsight() else denebHint(),
                modifier = Modifier.padding(top = 1.dp).size(18.dp),
            )
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    // The gateway sorts the feed by PRIORITY first and recency second,
                    // so an urgent card from this morning outranks a routine one from
                    // ten minutes ago. Without a marker the timestamps read as
                    // shuffled — the dot is what makes that ordering legible.
                    //
                    // Warm accent, NOT the M3 error red the mail row uses: this marks
                    // ~29% of live cards (161 of 557 measured), and at that density an
                    // alarm red repeats into noise and drowns the monochrome base. It
                    // is also not an error — it is a standing priority band. The
                    // restrained warm accent is one of the two sanctioned colors.
                    Text(
                        text = title,
                        style = titleStyle,
                        color = MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    val stamp = relativeTime(item.createdAtMs)
                    if (stamp.isNotEmpty()) {
                        Text(
                            text = stamp,
                            style = DenebType.meta,
                            color = denebHint(),
                            modifier = Modifier.padding(start = 8.dp),
                        )
                    }
                }
                // Summary spans the full row width — the quick actions no longer share
                // its line, so it wraps cleanly and shows more of the snippet.
                if (item.summary.isNotBlank()) {
                    Text(
                        text = item.summary,
                        style = DenebType.snippet,
                        color = denebHint(),
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                    )
                }
                // Quick actions appear only while the card is open, so collapsed rows
                // stay clean (icon · title · time · full-width summary). 보관 = archive
                // (ack → 읽음 section), 휴지통 = permanent delete; both ride onRunAction
                // (the gateway handles "trash" as a universal delete).
                if (expanded) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        FeedActionButton(Icons.Outlined.Archive, "보관") {
                            haptics.confirm()
                            onRunAction(item.id, "ack")
                        }
                        FeedActionButton(Icons.Outlined.Delete, "휴지통") {
                            haptics.confirm()
                            onRunAction(item.id, "trash")
                        }
                    }
                }
            }
        }
    }
}

@Composable
internal fun WorkFeedApprovalDialog(
    item: WorkFeedItem,
    action: WorkFeedAction,
    onDismiss: () -> Unit,
    onAnswer: (WorkFeedItem, String, String?, String?) -> Unit,
) {
    val haptics = rememberHaptics()
    val isReject = action.id == "approval:reject"
    var rejectionComment by remember(item.id, action.id) { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("${action.label}할까요?", style = DenebType.subject) },
        text = {
            Column {
                Text(
                    buildString {
                        append(item.title.ifBlank { "이 결재 문서" })
                        if (item.refId.isNotBlank()) append(" (doc ${item.refId})")
                        append("을(를) ${action.label}합니다. 그룹웨어에 즉시 반영됩니다.")
                    },
                    style = DenebType.body,
                )
                if (isReject) {
                    Spacer(Modifier.height(16.dp))
                    OutlinedTextField(
                        value = rejectionComment,
                        onValueChange = { rejectionComment = limitApprovalComment(it) },
                        label = { Text("반려 사유 (선택)", style = DenebType.meta) },
                        supportingText = {
                            Text(
                                "${approvalCommentCharacterCount(rejectionComment)}/$MaxApprovalCommentCharacters",
                                modifier = Modifier.fillMaxWidth(),
                                style = DenebType.meta,
                                color = denebHint(),
                                textAlign = TextAlign.End,
                            )
                        },
                        minLines = 3,
                        maxLines = 5,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    val comment = rejectionComment.trim().takeIf { isReject && it.isNotEmpty() }
                    onDismiss()
                    if (isReject) haptics.reject() else haptics.confirm()
                    onAnswer(item, action.label, action.id, comment)
                },
            ) { Text(action.label, style = DenebType.button) }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text("취소", style = DenebType.button)
            }
        },
    )
}

/**
 * Inline answer affordance for a question card (Toss-style). Renders the card's
 * options as tappable chips, or — when there are no fixed options — a free-text
 * reply field. Both route the answer back to the asking agent via [onAnswer]
 * (item, answerText, actionId?, comment?): a chip passes its actionId; the reply
 * passes null. Approval rejection can additionally carry a transient comment.
 * Shown under the expanded body of a card whose [WorkFeedItem.question] is true.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun WorkFeedAnswerBlock(
    item: WorkFeedItem,
    onAnswer: (WorkFeedItem, String, String?, String?) -> Unit,
) {
    val haptics = rememberHaptics()
    var pendingApproval by remember(item.id) { mutableStateOf<WorkFeedAction?>(null) }
    pendingApproval?.let { action ->
        WorkFeedApprovalDialog(
            item = item,
            action = action,
            onDismiss = { pendingApproval = null },
            onAnswer = onAnswer,
        )
    }
    Column(modifier = Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, bottom = 12.dp)) {
        if (item.actions.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                item.actions.forEach { action ->
                    AssistChip(
                        onClick = {
                            if (action.id.startsWith("approval:")) {
                                pendingApproval = action
                            } else {
                                haptics.confirm()
                                onAnswer(item, action.label, action.id, null)
                            }
                        },
                        label = { Text(action.label, style = DenebType.button) },
                        modifier = Modifier.handCursor(),
                    )
                }
            }
        } else {
            // free-text question (no fixed options): a reply field routed to the session.
            var text by remember(item.id) { mutableStateOf("") }
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    placeholder = { Text("답장…", style = DenebType.hint) },
                    modifier = Modifier.weight(1f),
                    minLines = 1,
                )
                Spacer(Modifier.width(8.dp))
                IconButton(
                    modifier = Modifier.handCursor(),
                    enabled = text.isNotBlank(),
                    onClick = {
                        haptics.confirm()
                        onAnswer(item, text.trim(), null, null)
                        text = ""
                    },
                ) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.Send,
                        contentDescription = "답장 보내기",
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
            }
        }
    }
}

/** Universal inbox-lifecycle kinds — already surfaced elsewhere (확인 quick
 *  button, long-press sheet) so the chip row never repeats them. */
internal val WorkFeedLifecycleKinds = setOf("open", "followup", "snooze", "ack", "answer", "trash")

/**
 * Card-specific operations promoted to chips under the expanded body (e.g. a
 * dream card's 전체/페이지별 되돌리기). Done operations drop off.
 */
internal fun WorkFeedItem.chipActions(): List<WorkFeedAction> = actions.filter { action ->
    val kind = action.kind.ifBlank { action.id }
    kind !in WorkFeedLifecycleKinds && action.status != "done"
}

/**
 * Chip row for a NON-question card's own actions (question cards render theirs
 * through [WorkFeedAnswerBlock]; groupware approvals keep their inline body
 * buttons). Revert-style actions confirm first — they rewrite wiki pages back
 * to a previous state.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun WorkFeedActionChips(
    item: WorkFeedItem,
    onRunAction: (String, String) -> Unit,
) {
    val chips = remember(item.id, item.actions) { item.chipActions() }
    if (chips.isEmpty()) return
    val haptics = rememberHaptics()
    var pendingRevert by remember(item.id) { mutableStateOf<WorkFeedAction?>(null) }
    pendingRevert?.let { action ->
        AlertDialog(
            onDismissRequest = { pendingRevert = null },
            title = { Text(action.label, style = DenebType.subject) },
            text = {
                Text(
                    "이 작업은 위키 페이지를 이전 상태로 되돌립니다. 계속할까요?",
                    style = DenebType.body,
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        haptics.confirm()
                        pendingRevert = null
                        onRunAction(item.id, action.id)
                    },
                ) { Text("되돌리기", style = DenebType.button) }
            },
            dismissButton = {
                TextButton(onClick = { pendingRevert = null }) { Text("취소", style = DenebType.button) }
            },
        )
    }
    FlowRow(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 12.dp, bottom = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        chips.forEach { action ->
            AssistChip(
                onClick = {
                    if (action.id.startsWith("dream:revert")) {
                        pendingRevert = action
                    } else {
                        haptics.confirm()
                        onRunAction(item.id, action.id)
                    }
                },
                label = { Text(action.label, style = DenebType.button) },
                modifier = Modifier.handCursor(),
            )
        }
    }
}

@Composable
internal fun WorkFeedActionSheetContent(
    item: WorkFeedItem,
    onFeedback: () -> Unit,
    onRewrite: () -> Unit,
    onAsk: () -> Unit,
) {
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else stripLeadingIcon(item.title)
    Column(Modifier.fillMaxWidth().padding(bottom = 24.dp)) {
        Text(
            title,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 12.dp),
        )
        if (item.summary.isNotBlank()) {
            Text(
                item.summary,
                style = DenebType.snippet,
                color = denebHint(),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(horizontal = 24.dp).padding(bottom = 12.dp),
            )
        }
        HorizontalDivider(color = denebHairline())
        // AI actions on this card's analysis. Inbox lifecycle (열기/완료/휴지통/나중에)
        // moved off this menu — tap the card to expand (= 열기), and the expanded row
        // carries the 보관/휴지통 quick buttons.
        WorkFeedSheetAction(Icons.Outlined.Edit, "정정·피드백", onOpen = onFeedback)
        WorkFeedSheetAction(Icons.Outlined.AutoAwesome, "다시 작성", onOpen = onRewrite)
        WorkFeedSheetAction(Icons.Outlined.QuestionAnswer, "해당 피드 질문", onOpen = onAsk)
    }
}

/**
 * Feedback input for a work-feed card (long-press → 정정·피드백). The user teaches the
 * agent — a wrong fact in the analysis, something it didn't know. On send, the
 * gateway annotates this card with the correction and runs one agent turn to fix
 * the durable wiki knowledge. Controls stay Material (field + buttons); the send is
 * fire-and-forget (the parent closes after the brief "sent" confirmation).
 */
@Composable
internal fun WorkFeedFeedbackSheetContent(
    item: WorkFeedItem,
    onSubmit: (String) -> Unit,
    onClose: () -> Unit,
) {
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else stripLeadingIcon(item.title)
    var text by remember { mutableStateOf("") }
    var sent by remember { mutableStateOf(false) }
    Column(
        Modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .padding(horizontal = 24.dp, vertical = 12.dp),
    ) {
        Text(
            "정정·피드백",
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
        )
        Text(
            title,
            style = DenebType.snippet,
            color = denebHint(),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(top = 2.dp),
        )
        Spacer(Modifier.height(12.dp))
        if (sent) {
            Text(
                "✓ 피드백을 보냈습니다. 관련 지식을 바로잡고 카드에 반영할게요.",
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.padding(vertical = 12.dp),
            )
            // Self-dismiss after the user has seen the confirmation.
            LaunchedEffect(Unit) {
                delay(1500)
                onClose()
            }
        } else {
            Text(
                "이 카드 분석에서 틀렸거나 에이전트가 몰랐던 내용을 알려주세요.",
                style = DenebType.snippet,
                color = denebHint(),
            )
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                placeholder = { Text("예: 이 거래처 담당자는 김 부장이 아니라 이서연 차장입니다") },
                modifier = Modifier.fillMaxWidth().heightIn(min = 104.dp),
                minLines = 3,
            )
            Spacer(Modifier.height(12.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = onClose) { Text("취소") }
                Spacer(Modifier.width(8.dp))
                Button(
                    onClick = {
                        onSubmit(text.trim())
                        sent = true
                    },
                    enabled = text.isNotBlank(),
                ) { Text("보내기") }
            }
        }
    }
}

@Composable
private fun WorkFeedSheetAction(
    icon: ImageVector,
    label: String,
    destructive: Boolean = false,
    onOpen: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onOpen)
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val color = if (destructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(16.dp))
        Text(label, style = DenebType.rowTitle, color = if (destructive) color else MaterialTheme.colorScheme.onBackground)
    }
}

/** Drop a leading emoji/symbol run from a card title so it isn't shown twice next to
 *  the row's source icon ("📬 메일 분석" → "메일 분석"). Stops at the first letter/digit
 *  (Hangul/Latin/CJK/number); returns the original if stripping would empty it. */
private fun stripLeadingIcon(s: String): String {
    var i = 0
    while (i < s.length && !s[i].isLetterOrDigit()) i++
    return s.substring(i).trimStart().ifBlank { s }
}

internal enum class WorkFeedSourceIcon {
    MAIL,
    IMAGE,
    AUDIO,
    CONTACTS,
    APPROVAL,
    BOARD,
    QUESTION,
    MEETING,
    REPORT,
    DREAM,
    DIGEST,
    TRUST,
    GENESIS,
    INTERVIEW,
    HEARTBEAT,
    LOG,
    DOCUMENT,
}

internal data class WorkFeedSourcePresentation(
    val icon: WorkFeedSourceIcon,
    val label: String?,
)

/**
 * Source → (icon, a11y label). Identity rides the GLYPH, not a per-source color:
 * the feed's palette is deliberately monochrome plus the single warm urgent
 * accent (see the tint note in WorkFeedRow), so cards from the dreamer, the
 * Trust Inbox, and question cards are told apart by shape while the color
 * channel keeps meaning exactly one thing — urgency. Labels mirror the
 * gateway's source strings (internal/domain/workfeed/store.go and the
 * per-producer literals); unknown sources keep the generic document fallback.
 */
internal fun workFeedSourcePresentation(source: String): WorkFeedSourcePresentation = when (source.trim()) {
    "mail_report" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.MAIL, "메일")
    "capture_image" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.IMAGE, "이미지")
    "capture_audio" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.AUDIO, "음성")
    "capture_contacts" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.CONTACTS, "연락처")
    "capture_document" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.DOCUMENT, "문서")
    "groupware-approval" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.APPROVAL, "전자결재")
    "groupware-board" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.BOARD, "공지")
    "proactive" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.REPORT, "리포트")
    "meeting_report" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.MEETING, "회의")
    "dream" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.DREAM, "드림")
    "dream-digest" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.DIGEST, "기억 다이제스트")
    "self-correction" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.TRUST, "자기교정")
    "deal_question" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.QUESTION, "질문")
    "kb-interview" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.INTERVIEW, "인터뷰")
    "genesis-meta", "genesis-evolve-verdict", "genesis-ladder" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.GENESIS, "자가개선")
    "heartbeat" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.HEARTBEAT, "하트비트")
    "system_log" -> WorkFeedSourcePresentation(WorkFeedSourceIcon.LOG, "로그")
    else -> WorkFeedSourcePresentation(WorkFeedSourceIcon.DOCUMENT, null)
}

/** Leading icon by card source: an envelope for mail reports, a checked document
 *  for electronic approvals, a glyph per autonomous producer (dreamer, digest,
 *  trust inbox, questions, genesis watches), and a concrete glyph for each
 *  capture kind. */
@Composable
private fun sourcePainter(source: WorkFeedSourceIcon): Painter = when (source) {
    WorkFeedSourceIcon.MAIL -> rememberVectorPainter(Icons.Outlined.MailOutline)
    WorkFeedSourceIcon.IMAGE -> painterResource(Res.drawable.ic_image)
    WorkFeedSourceIcon.AUDIO -> rememberVectorPainter(Icons.Filled.Mic)
    WorkFeedSourceIcon.CONTACTS -> rememberVectorPainter(Icons.Filled.Person)
    WorkFeedSourceIcon.APPROVAL -> rememberVectorPainter(Icons.Filled.TaskAlt)
    WorkFeedSourceIcon.BOARD -> rememberVectorPainter(Icons.Filled.PushPin)
    WorkFeedSourceIcon.QUESTION -> rememberVectorPainter(Icons.Outlined.QuestionAnswer)
    WorkFeedSourceIcon.MEETING -> rememberVectorPainter(Icons.Outlined.KeyboardVoice)
    WorkFeedSourceIcon.REPORT -> rememberVectorPainter(Icons.Outlined.Article)
    WorkFeedSourceIcon.DREAM -> rememberVectorPainter(Icons.Outlined.AutoAwesome)
    WorkFeedSourceIcon.DIGEST -> rememberVectorPainter(Icons.Outlined.Book)
    WorkFeedSourceIcon.TRUST -> rememberVectorPainter(Icons.Filled.Verified)
    WorkFeedSourceIcon.GENESIS -> rememberVectorPainter(Icons.Outlined.Autorenew)
    WorkFeedSourceIcon.INTERVIEW -> rememberVectorPainter(Icons.Filled.School)
    WorkFeedSourceIcon.HEARTBEAT -> rememberVectorPainter(Icons.Outlined.Bolt)
    WorkFeedSourceIcon.LOG -> rememberVectorPainter(Icons.Filled.Terminal)
    WorkFeedSourceIcon.DOCUMENT -> painterResource(Res.drawable.ic_file)
}

/** A compact trailing quick-action icon button (보관 / 휴지통), muted to denebHint. */
@Composable
private fun FeedActionButton(icon: ImageVector, label: String, onClick: () -> Unit) {
    IconButton(
        modifier = Modifier.handCursor().size(32.dp),
        onClick = onClick,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = label,
            tint = denebHint(),
            modifier = Modifier.size(16.dp),
        )
    }
}

/** Short Korean relative time ("방금" / "N분 전" / "N시간 전" / "N일 전"). Blank for
 *  missing/future timestamps so the row simply omits the stamp. */
private fun relativeTime(epochMs: Long): String {
    if (epochMs <= 0L) return ""
    val diff = Clock.System.now().toEpochMilliseconds() - epochMs
    return when {
        diff < 0L -> ""
        diff < 60_000L -> "방금"
        diff < 3_600_000L -> "${diff / 60_000L}분 전"
        diff < 86_400_000L -> "${diff / 3_600_000L}시간 전"
        else -> "${diff / 86_400_000L}일 전"
    }
}
