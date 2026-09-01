@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.ChatActions
import ai.deneb.ui.chat.ConversationSummary
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.handCursor
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
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
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.chat_history_empty
import deneb.composeapp.generated.resources.chat_history_heartbeat_label
import deneb.composeapp.generated.resources.chat_history_more
import deneb.composeapp.generated.resources.chat_history_title
import deneb.composeapp.generated.resources.snackbar_conversation_deleted
import deneb.composeapp.generated.resources.snackbar_undo
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.toImmutableList
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.format.char
import kotlinx.datetime.toLocalDateTime
import org.jetbrains.compose.resources.stringResource

private val dateFormat = LocalDate.Format {
    year()
    char('년')
    char(' ')
    monthNumber()
    char('월')
    char(' ')
    day()
    char('일')
}

/**
 * The right-side session selector, in the left drawer's typographic idiom
 * ([DenebDrawerSheet]): recent Deneb conversations as big ultralight words — the
 * active one in full weight — with a quiet date below. Search stays behind a
 * top-right magnifying glass; pin and × appear only after a long-press on the
 * session name. No cards or borders, so left=nav and right=sessions read as one
 * family. Opened by the top-bar session button or a right-edge swipe.
 */
@Composable
fun DenebSessionDrawerSheet(
    conversations: ImmutableList<ConversationSummary>,
    hasMoreConversations: Boolean,
    currentConversationId: String?,
    pendingConversationDeletion: String?,
    searchHits: ImmutableList<ConversationSummary> = kotlinx.collections.immutable.persistentListOf(),
    actions: ChatActions,
    onClose: () -> Unit,
    initiallySearchOpen: Boolean = false,
    initiallyRevealedSessionId: String? = null,
) {
    ModalDrawerSheet(drawerContainerColor = MaterialTheme.colorScheme.background) {
        val snackbarHostState = remember { SnackbarHostState() }
        val deletedMessage = stringResource(Res.string.snackbar_conversation_deleted)
        val undoLabel = stringResource(Res.string.snackbar_undo)

        LaunchedEffect(pendingConversationDeletion) {
            if (pendingConversationDeletion == null) return@LaunchedEffect
            snackbarHostState.currentSnackbarData?.dismiss()
            val result = snackbarHostState.showSnackbar(
                message = deletedMessage,
                actionLabel = undoLabel,
                duration = SnackbarDuration.Short,
            )
            if (result == SnackbarResult.ActionPerformed) {
                actions.undoDeleteConversation()
            }
        }

        Box(modifier = Modifier.fillMaxSize()) {
            Column(modifier = Modifier.fillMaxSize()) {
                val haptics = rememberHaptics()
                val focusManager = LocalFocusManager.current
                var searchOpen by remember { mutableStateOf(initiallySearchOpen) }
                var searchQuery by remember { mutableStateOf("") }
                var revealedSessionId by remember { mutableStateOf(initiallyRevealedSessionId) }
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(start = 28.dp, end = 16.dp, top = 40.dp, bottom = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = stringResource(Res.string.chat_history_title),
                        style = DenebType.rowTitle,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.weight(1f),
                    )
                    IconButton(
                        modifier = Modifier.size(40.dp),
                        onClick = {
                            haptics.tap()
                            if (searchOpen) {
                                focusManager.clearFocus()
                                searchOpen = false
                                if (searchQuery.isNotEmpty()) {
                                    searchQuery = ""
                                    actions.searchConversations("")
                                }
                            } else {
                                revealedSessionId = null
                                searchOpen = true
                            }
                        },
                    ) {
                        Icon(
                            Icons.Outlined.Search,
                            contentDescription = if (searchOpen) "대화 검색 닫기" else "대화 검색 열기",
                            tint = if (searchOpen) {
                                MaterialTheme.colorScheme.primary
                            } else {
                                denebHint()
                            },
                        )
                    }
                }
                AnimatedVisibility(
                    visible = searchOpen,
                    enter = denebExpandIn,
                    exit = denebShrinkOut,
                ) {
                    DenebUnderlineSearchField(
                        query = searchQuery,
                        onQueryChange = { next ->
                            searchQuery = next
                            actions.searchConversations(next)
                        },
                        placeholder = "대화 검색",
                        textStyle = DenebType.body,
                        clearable = true,
                        autoFocus = true,
                        modifier = Modifier.padding(start = 28.dp, end = 28.dp, bottom = 8.dp),
                    )
                }

                if (conversations.isEmpty() && searchHits.isEmpty() && searchQuery.isBlank()) {
                    Text(
                        text = stringResource(Res.string.chat_history_empty),
                        style = DenebType.body,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = 28.dp, vertical = 16.dp),
                    )
                } else {
                    val needle = searchQuery.trim()
                    val merged = if (needle.isEmpty()) {
                        conversations
                    } else {
                        val local = conversations.filter {
                            it.title.contains(needle, ignoreCase = true) || it.id.contains(needle, ignoreCase = true)
                        }
                        val extra = searchHits.filter { hit -> local.none { it.id == hit.id } }
                        (local + extra).toImmutableList()
                    }
                    val chats = merged.filter { !isSystemSession(it.id) && !isWorkCardSession(it.id) }
                    val workCards = merged.filter { isWorkCardSession(it.id) }
                    val systemSessions = merged.filter { isSystemSession(it.id) }
                    var renameTarget by remember { mutableStateOf<ConversationSummary?>(null) }
                    var workExpanded by remember {
                        mutableStateOf(currentConversationId != null && isWorkCardSession(currentConversationId))
                    }
                    var systemExpanded by remember { mutableStateOf(false) }
                    renameTarget?.let { target ->
                        RenameConversationDialog(
                            initial = target.title,
                            onSave = { label ->
                                actions.renameConversation(target.id, label)
                                renameTarget = null
                                revealedSessionId = null
                            },
                            onDismiss = { renameTarget = null },
                        )
                    }
                    if (merged.isEmpty() && needle.isNotEmpty()) {
                        Text(
                            text = "검색 결과가 없습니다",
                            style = DenebType.body,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(horizontal = 28.dp, vertical = 16.dp),
                        )
                    }
                    LazyColumn(
                        state = rememberLazyListState(),
                        contentPadding = PaddingValues(horizontal = 28.dp),
                    ) {
                        items(chats, key = { it.id }) { conversation ->
                            SessionItem(
                                conversation = conversation,
                                isActive = conversation.id == currentConversationId,
                                actionsVisible = revealedSessionId == conversation.id,
                                onClick = {
                                    actions.loadConversation(conversation.id)
                                    onClose()
                                },
                                onLongClick = {
                                    revealedSessionId = if (revealedSessionId == conversation.id) {
                                        null
                                    } else {
                                        conversation.id
                                    }
                                },
                                // The 업무 home (client:main) is the permanent base
                                // conversation where proactive reports are mirrored —
                                // no × so it can't be deleted out from under them.
                                // Every other conversation is deletable.
                                onDelete = if (conversation.id == HOME_SESSION_ID) {
                                    null
                                } else {
                                    { actions.deleteConversation(conversation.id) }
                                },
                                onRename = { renameTarget = conversation },
                                onPin = if (conversation.id == HOME_SESSION_ID) {
                                    null
                                } else {
                                    {
                                        actions.pinConversation(conversation.id, !conversation.pinned)
                                        revealedSessionId = null
                                    }
                                },
                                onResetModel = if (conversation.model.isBlank()) {
                                    null
                                } else {
                                    { actions.resetConversationModel(conversation.id) }
                                },
                            )
                        }
                        // Feed-card side chats (client:main:wf-*) fold so they
                        // don't bury the user's own conversations.
                        if (workCards.isNotEmpty()) {
                            item(key = "__work_card_folder__") {
                                SessionFolderHeader(
                                    label = "카드 대화",
                                    count = workCards.size,
                                    expanded = workExpanded,
                                    onToggle = { workExpanded = !workExpanded },
                                    collapseLabel = "카드 대화 접기",
                                    expandLabel = "카드 대화 펼치기",
                                )
                            }
                            if (workExpanded) {
                                items(workCards, key = { it.id }) { conversation ->
                                    SessionItem(
                                        conversation = conversation,
                                        isActive = conversation.id == currentConversationId,
                                        actionsVisible = revealedSessionId == conversation.id,
                                        onClick = {
                                            actions.loadConversation(conversation.id)
                                            onClose()
                                        },
                                        onLongClick = {
                                            revealedSessionId = if (revealedSessionId == conversation.id) {
                                                null
                                            } else {
                                                conversation.id
                                            }
                                        },
                                        onDelete = { actions.deleteConversation(conversation.id) },
                                        onRename = { renameTarget = conversation },
                                        onPin = {
                                            actions.pinConversation(conversation.id, !conversation.pinned)
                                            revealedSessionId = null
                                        },
                                        onResetModel = if (conversation.model.isBlank()) {
                                            null
                                        } else {
                                            { actions.resetConversationModel(conversation.id) }
                                        },
                                    )
                                }
                            }
                        }
                        // Machine-driven sessions (cron runs, system, boot) fold into
                        // one collapsible group so they don't bury the real chats above.
                        if (systemSessions.isNotEmpty()) {
                            item(key = "__system_folder__") {
                                SessionFolderHeader(
                                    label = "예약·시스템 세션",
                                    count = systemSessions.size,
                                    expanded = systemExpanded,
                                    onToggle = { systemExpanded = !systemExpanded },
                                    collapseLabel = "예약·시스템 세션 접기",
                                    expandLabel = "예약·시스템 세션 펼치기",
                                )
                            }
                            if (systemExpanded) {
                                items(systemSessions, key = { it.id }) { conversation ->
                                    SessionItem(
                                        conversation = conversation,
                                        isActive = conversation.id == currentConversationId,
                                        actionsVisible = revealedSessionId == conversation.id,
                                        onClick = {
                                            actions.loadConversation(conversation.id)
                                            onClose()
                                        },
                                        onLongClick = {
                                            revealedSessionId = if (revealedSessionId == conversation.id) {
                                                null
                                            } else {
                                                conversation.id
                                            }
                                        },
                                        onDelete = { actions.deleteConversation(conversation.id) },
                                        onRename = { renameTarget = conversation },
                                        onPin = {
                                            actions.pinConversation(conversation.id, !conversation.pinned)
                                            revealedSessionId = null
                                        },
                                        onResetModel = if (conversation.model.isBlank()) {
                                            null
                                        } else {
                                            { actions.resetConversationModel(conversation.id) }
                                        },
                                    )
                                }
                            }
                        }
                        // Older conversations live on the gateway beyond this page.
                        // Without this the drawer just ended, and everything past the
                        // first page was unreachable although its transcript is intact.
                        if (hasMoreConversations) {
                            item {
                                TextButton(
                                    onClick = actions.loadMoreConversations,
                                    modifier = Modifier.padding(vertical = 8.dp),
                                ) {
                                    Text(
                                        text = stringResource(Res.string.chat_history_more),
                                        style = DenebType.button,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                }
                            }
                        }
                        item { Spacer(Modifier.height(40.dp)) }
                    }
                }
            }

            SnackbarHost(
                hostState = snackbarHostState,
                modifier = Modifier.align(Alignment.BottomCenter).padding(16.dp),
            ) { data ->
                Snackbar(snackbarData = data)
            }
        }
    }
}

@Composable
private fun SessionItem(
    conversation: ConversationSummary,
    isActive: Boolean,
    actionsVisible: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onDelete: (() -> Unit)?,
    onRename: () -> Unit,
    onPin: (() -> Unit)? = null,
    onResetModel: (() -> Unit)? = null,
) {
    val haptics = rememberHaptics()
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .denebPressable(
                    onClick = {
                        onClick()
                    },
                    onLongClick = onLongClick,
                )
                .handCursor(),
        ) {
            if (conversation.isHeartbeat) {
                Text(
                    text = stringResource(Res.string.chat_history_heartbeat_label),
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
            if (conversation.title.isNotEmpty()) {
                Text(
                    text = conversation.title,
                    // subject (22 / Light) carries the row; the active row steps up to
                    // Normal — the old ExtraLight rest state read as a hairline on
                    // Korean session titles.
                    style = DenebType.subject,
                    fontWeight = if (isActive) FontWeight.Normal else null,
                    color = if (isActive) {
                        MaterialTheme.colorScheme.onBackground
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            val date = formatDate(conversation.updatedAt)
            val dateLine = listOfNotNull(
                date.takeIf { it.isNotEmpty() },
                "핀".takeIf { conversation.pinned },
            ).joinToString(" · ")
            if (dateLine.isNotEmpty()) {
                Text(
                    text = dateLine,
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
            if (conversation.id == HOME_SESSION_ID) {
                Text(
                    text = "선제 보고가 모이는 업무 홈",
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
            val modelLine = when {
                conversation.model.isNotBlank() -> conversation.model.substringAfterLast('/')
                conversation.snippet.isNotBlank() -> conversation.snippet
                else -> null
            }
            if (modelLine != null) {
                Text(
                    text = modelLine,
                    style = DenebType.meta,
                    color = denebHint(),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        AnimatedVisibility(
            visible = actionsVisible,
            enter = denebExpandIn,
            exit = denebShrinkOut,
        ) {
            SessionRowActions(
                pinned = conversation.pinned,
                onPin = onPin,
                onRename = onRename,
                onResetModel = onResetModel,
                onDelete = onDelete,
            )
        }
    }
}

@Composable
private fun SessionRowActions(
    pinned: Boolean,
    onPin: (() -> Unit)?,
    onRename: () -> Unit,
    onResetModel: (() -> Unit)?,
    onDelete: (() -> Unit)?,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onPin != null) {
            SessionAction(
                label = if (pinned) "고정" else "핀",
                onClickLabel = if (pinned) "고정 해제" else "위에 고정",
                emphasized = pinned,
                onClick = onPin,
            )
        }
        SessionAction(label = "이름", onClickLabel = "이름 변경", onClick = onRename)
        if (onResetModel != null) {
            SessionAction(label = "기본", onClickLabel = "기본 모델로", onClick = onResetModel)
        }
        if (onDelete != null) {
            SessionAction(label = "×", onClickLabel = "대화 삭제", onClick = onDelete)
        }
    }
}

@Composable
private fun SessionAction(
    label: String,
    onClickLabel: String,
    emphasized: Boolean = false,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .padding(end = 16.dp)
            .height(44.dp)
            .clickable(onClickLabel = onClickLabel, role = Role.Button, onClick = onClick)
            .handCursor(),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            style = if (label == "×") DenebType.rowTitle else DenebType.meta,
            color = if (emphasized) {
                MaterialTheme.colorScheme.onBackground
            } else {
                denebHint()
            },
        )
    }
}

// Formatted in the device's zone, not UTC. kotlinx-datetime's Instant.format
// defaults to UtcOffset.ZERO, which in KST put everything saved between midnight
// and 09:00 under the previous day — the drawer's date headers disagreed with the
// message timestamps inside them (MessageActionsSheet already converts locally).
private fun formatDate(epochMillis: Long): String = try {
    dateFormat.format(
        kotlin.time.Instant.fromEpochMilliseconds(epochMillis)
            .toLocalDateTime(TimeZone.currentSystemDefault())
            .date,
    )
} catch (_: Exception) {
    ""
}

// The permanent base conversation (업무 home), keyed under the native client and
// always pinned to the top of the drawer. It has no delete affordance — proactive
// reports are mirrored here, so it must not be removable.
internal const val HOME_SESSION_ID = "client:main"

// A session is a real user conversation only when it's keyed under the native
// client (client:main, client:main:<uuid>). Everything else is machine-driven —
// cron runs, the boot turn, and the system/autonomous/curator/dream/genesis/
// heartbeat background turns — and folds into one collapsible group
// below the chats. Whitelisting the user prefix (rather than blacklisting the
// known machine ones) means a newly-added background session kind can never leak
// into the chat list, which is what made the grouping look intermittent.
internal fun isSystemSession(id: String): Boolean = when (id.substringBefore(':', id)) {
    // user chats (client:) plus the legacy chat: namespace stay top-level
    "client", "chat" -> false

    else -> true
}

// Feed-card side chats are keyed client:main:wf-<slug>. They stay under the
// client: prefix (so isSystemSession is false) but fold into their own group.
internal fun isWorkCardSession(id: String): Boolean = id.substringAfterLast(':').startsWith("wf-")

private const val RENAME_LABEL_MAX = 60

@Composable
private fun RenameConversationDialog(
    initial: String,
    onSave: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var draft by remember(initial) { mutableStateOf(initial) }
    val trimmed = draft.trim()
    val haptics = rememberHaptics()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("이름 변경", style = DenebType.rowTitle) },
        confirmButton = {
            TextButton(
                enabled = trimmed.isNotEmpty(),
                onClick = {
                    haptics.confirm()
                    onSave(trimmed)
                },
            ) { Text("저장") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
        text = {
            OutlinedTextField(
                value = draft,
                onValueChange = { next ->
                    draft = if (next.length <= RENAME_LABEL_MAX) next else next.take(RENAME_LABEL_MAX)
                },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
            )
        },
    )
}

// Collapsible header for a drawer folder, in the text-first idiom:
// a ▸/▾ glyph + label + count, no Material expander chrome.
@Composable
private fun SessionFolderHeader(
    label: String,
    count: Int,
    expanded: Boolean,
    onToggle: () -> Unit,
    collapseLabel: String,
    expandLabel: String,
) {
    val haptics = rememberHaptics()
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(
                onClick = {
                    haptics.toggle(!expanded)
                    onToggle()
                },
                onClickLabel = if (expanded) collapseLabel else expandLabel,
                role = Role.Button,
                // The click means expand/collapse, not a plain tap — this row owns
                // the richer type and turns the primitive's tap off.
                haptic = false,
            )
            .handCursor()
            .padding(vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = if (expanded) "▾" else "▸",
            fontSize = 14.sp,
            color = denebHint(),
            modifier = Modifier.padding(end = 10.dp),
        )
        Text(
            text = label,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = "$count",
            style = DenebType.meta,
            color = denebHint(),
            modifier = Modifier.padding(start = 8.dp),
        )
    }
}
