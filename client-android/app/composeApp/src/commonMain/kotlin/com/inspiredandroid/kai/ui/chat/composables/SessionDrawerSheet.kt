@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai.ui.chat.composables

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
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import com.inspiredandroid.kai.ui.chat.ChatActions
import com.inspiredandroid.kai.ui.chat.ConversationSummary
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.handCursor
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.chat_history_empty
import kai.composeapp.generated.resources.chat_history_heartbeat_label
import kai.composeapp.generated.resources.chat_history_title
import kai.composeapp.generated.resources.snackbar_conversation_deleted
import kai.composeapp.generated.resources.snackbar_undo
import kotlinx.collections.immutable.ImmutableList
import kotlinx.datetime.format
import kotlinx.datetime.format.DateTimeComponents.Companion.Format
import kotlinx.datetime.format.MonthNames
import kotlinx.datetime.format.char
import org.jetbrains.compose.resources.stringResource

private val dateFormat = Format {
    day()
    char(' ')
    monthName(MonthNames.ENGLISH_ABBREVIATED)
    char(' ')
    year()
}

/**
 * The right-side session selector, in the left drawer's typographic idiom
 * ([DenebDrawerSheet]): recent Deneb conversations as big ultralight words — the
 * active one in full weight, a live (interactive) one in the primary color — with
 * a quiet date below and a faint × to delete. No cards or borders, so left=nav and
 * right=sessions read as one family. Opened by the top-bar session button or a
 * right-edge swipe.
 */
@Composable
fun DenebSessionDrawerSheet(
    conversations: ImmutableList<ConversationSummary>,
    currentConversationId: String?,
    pendingConversationDeletion: String?,
    actions: ChatActions,
    onClose: () -> Unit,
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
                // Small quiet header, mirroring the topic drawer's "topics" label.
                Text(
                    text = stringResource(Res.string.chat_history_title),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.Normal,
                    letterSpacing = 0.01.em,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 28.dp, end = 28.dp, top = 40.dp, bottom = 12.dp),
                )

                if (conversations.isEmpty()) {
                    Text(
                        text = stringResource(Res.string.chat_history_empty),
                        fontSize = 15.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = 28.dp, vertical = 16.dp),
                    )
                } else {
                    LazyColumn(
                        state = rememberLazyListState(),
                        contentPadding = PaddingValues(horizontal = 28.dp),
                    ) {
                        items(conversations, key = { it.id }) { conversation ->
                            SessionItem(
                                conversation = conversation,
                                isActive = conversation.id == currentConversationId,
                                onClick = {
                                    actions.loadConversation(conversation.id)
                                    onClose()
                                },
                                onDelete = { actions.deleteConversation(conversation.id) },
                            )
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
    onClick: () -> Unit,
    onDelete: () -> Unit,
) {
    val haptics = rememberHaptics()
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { haptics.tap(); onClick() }
            .handCursor()
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            if (conversation.isHeartbeat) {
                Text(
                    text = stringResource(Res.string.chat_history_heartbeat_label),
                    fontSize = 11.sp,
                    letterSpacing = 0.06.em,
                    color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.7f),
                )
            }
            if (conversation.title.isNotEmpty()) {
                Text(
                    text = conversation.title,
                    fontSize = 21.sp,
                    lineHeight = 27.sp,
                    fontWeight = if (isActive || conversation.isInteractive) FontWeight.Normal else FontWeight.ExtraLight,
                    letterSpacing = (-0.02).em,
                    color = when {
                        isActive -> MaterialTheme.colorScheme.onBackground
                        conversation.isInteractive -> MaterialTheme.colorScheme.primary
                        else -> MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Text(
                text = formatDate(conversation.updatedAt),
                fontSize = 12.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f),
            )
        }
        // Faint × to delete (undo via snackbar), keeping the row text-first. The 44dp
        // box gives a real touch target + a TalkBack label without a Material icon.
        Box(
            modifier = Modifier
                .padding(start = 8.dp)
                .size(44.dp)
                .clickable(onClickLabel = "대화 삭제", role = Role.Button) { onDelete() }
                .handCursor(),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = "×",
                fontSize = 18.sp,
                color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.55f),
            )
        }
    }
}

private fun formatDate(epochMillis: Long): String = try {
    kotlin.time.Instant.fromEpochMilliseconds(epochMillis).format(dateFormat)
} catch (_: Exception) {
    ""
}
