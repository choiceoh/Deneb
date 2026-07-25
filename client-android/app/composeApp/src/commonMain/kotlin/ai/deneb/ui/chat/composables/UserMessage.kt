package ai.deneb.ui.chat.composables

import ai.deneb.data.Attachment
import ai.deneb.shareTextToApps
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.LocalShowFullScreenImage
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import ai.deneb.ui.icons.filled.ContentCopy
import ai.deneb.ui.icons.filled.SelectAll
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SuggestionChip
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_file
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.coroutines.launch
import org.jetbrains.compose.resources.painterResource

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun UserMessage(
    message: String,
    attachments: ImmutableList<Attachment> = persistentListOf(),
    timestampMs: Long = 0,
    // 마지막 사용자 메시지에서만 non-null — 편집 후 다시 보내기 (regenerate 시맨틱).
    onEditResend: ((String) -> Unit)? = null,
) {
    val showFullScreen = LocalShowFullScreenImage.current
    val haptics = rememberHaptics()
    val clipboard = LocalClipboardManager.current
    val scope = rememberCoroutineScope()
    // Long-press → action sheet (복사·텍스트 선택·공유·보낸 시각). The old inline
    // SelectionContainer swallowed long-press for drag-selection, which left the
    // bubble with ZERO discoverable actions — the benchmark grammar is a menu on
    // long-press, with text selection as a menu entry (dialog).
    var sheetOpen by remember { mutableStateOf(false) }
    var selectTextOpen by remember { mutableStateOf(false) }
    var editOpen by remember { mutableStateOf(false) }
    // Borderless gray bubble for "my message": a neutral surfaceVariant container in
    // both light (#E1E7EE) and OLED dark (#2A2F35) — no accent wash, no hairline ring.
    val cs = MaterialTheme.colorScheme
    val bubbleShape = RoundedCornerShape(18.dp, 18.dp, 4.dp, 18.dp)
    val bubbleColor = cs.surfaceVariant
    val bubbleText = cs.onSurfaceVariant

    if (sheetOpen) {
        MessageActionsSheet(
            timestampMs = timestampMs,
            actions = buildList {
                add(
                    MessageSheetAction(Icons.Filled.ContentCopy, "복사") {
                        clipboard.setText(AnnotatedString(message))
                    },
                )
                add(MessageSheetAction(Icons.Filled.SelectAll, "텍스트 선택") { selectTextOpen = true })
                if (onEditResend != null) {
                    add(MessageSheetAction(Icons.Filled.Edit, "편집 후 다시 보내기") { editOpen = true })
                }
                add(MessageSheetAction(Icons.Filled.Share, "공유") { scope.launch { shareTextToApps(message) } })
            },
            onDismiss = { sheetOpen = false },
        )
    }
    if (selectTextOpen) {
        SelectTextDialog(text = message, onDismiss = { selectTextOpen = false })
    }
    if (editOpen && onEditResend != null) {
        EditResendDialog(
            initial = message,
            onSend = { edited ->
                editOpen = false
                onEditResend(edited)
            },
            onDismiss = { editOpen = false },
        )
    }

    BoxWithConstraints(Modifier.fillMaxWidth()) {
        // Cap the bubble so a long message hugs the right instead of stretching to
        // the left edge: ~80% of the available width on phones, with an absolute
        // ceiling so it doesn't sprawl on wide desktop. Short messages still size
        // to their content (the trailing Spacer keeps it right-aligned).
        val bubbleMax = minOf(maxWidth * 0.80f, 520.dp)
        Row(Modifier.fillMaxWidth().padding(16.dp)) {
            Spacer(Modifier.weight(1f))
            Column(
                modifier = Modifier
                    .widthIn(max = bubbleMax)
                    .background(bubbleColor, bubbleShape)
                    // Long-press only (no tap ripple — a tap on a sent message does
                    // nothing, matching the benchmark apps). Manual long-press haptic
                    // because this is a raw gesture detector, not combinedClickable.
                    .pointerInput(message) {
                        detectTapGestures(onLongPress = {
                            haptics.longPress()
                            sheetOpen = true
                        })
                    }
                    // Messenger-tight padding so the bubble hugs the text instead of
                    // ballooning around it — horizontal a touch more than vertical
                    // (was a roomy uniform 16dp, which read oversized for the font).
                    .padding(horizontal = 14.dp, vertical = 9.dp),
                horizontalAlignment = Alignment.End,
            ) {
                val images = attachments.filter { it.mimeType.startsWith("image/") }
                val others = attachments.filter { !it.mimeType.startsWith("image/") }
                for (att in images) {
                    val imageBitmap = rememberDecodedImage(att.data)
                    if (imageBitmap != null) {
                        Image(
                            bitmap = imageBitmap,
                            contentDescription = "첨부 이미지",
                            modifier = Modifier
                                .widthIn(max = 200.dp)
                                .clip(RoundedCornerShape(8.dp))
                                .handCursor()
                                .clickable(onClickLabel = "확대") {
                                    haptics.tap()
                                    showFullScreen(imageBitmap, decodeBase64BytesOrNull(att.data))
                                },
                            contentScale = ContentScale.FillWidth,
                        )
                        Spacer(Modifier.height(8.dp))
                    }
                }
                if (others.isNotEmpty()) {
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        for (att in others) {
                            SuggestionChip(
                                onClick = {},
                                icon = {
                                    Icon(
                                        modifier = Modifier.size(16.dp),
                                        painter = painterResource(Res.drawable.ic_file),
                                        contentDescription = null,
                                        tint = MaterialTheme.colorScheme.onSecondaryContainer,
                                    )
                                },
                                label = { Text(truncateFileName(att.fileName ?: att.mimeType)) },
                            )
                        }
                    }
                    if (message.isNotEmpty()) {
                        Spacer(Modifier.height(8.dp))
                    }
                }
                if (message.isNotEmpty()) {
                    // DELIBERATE Material holdout in an otherwise DenebType file:
                    // the 14sp here is a chosen RELATIONSHIP, not a default — it sits
                    // one step under the assistant's bodyLarge so the user's own words
                    // read as the quieter half of the exchange. DenebType.body is 15sp
                    // and would flatten that pairing.
                    Text(
                        text = message,
                        style = MaterialTheme.typography.bodyMedium,
                        color = bubbleText,
                    )
                }
            }
        }
    }
}

// 텍스트 선택 — the long-press menu's selection entry. A dedicated dialog with a
// SelectionContainer replaces the old inline drag-selection (which consumed the
// bubble's long-press and offered no discoverable affordance).
@Composable
internal fun SelectTextDialog(text: String, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = onDismiss) { Text("닫기") } },
        text = {
            SelectionContainer {
                Text(
                    text = text,
                    style = DenebType.body,
                    modifier = Modifier.verticalScroll(rememberScrollState()),
                )
            }
        },
    )
}

// 편집 후 다시 보내기 — 마지막 질문을 고쳐 재질의한다. 보내기는 텍스트가 비어 있지
// 않을 때만; 취소는 아무것도 바꾸지 않는다.
@Composable
internal fun EditResendDialog(initial: String, onSend: (String) -> Unit, onDismiss: () -> Unit) {
    var draft by remember(initial) { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(
                enabled = draft.trim().isNotEmpty(),
                onClick = { onSend(draft.trim()) },
            ) { Text("보내기") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("취소") } },
        text = {
            OutlinedTextField(
                value = draft,
                onValueChange = { draft = it },
                modifier = Modifier.fillMaxWidth(),
                minLines = 2,
                maxLines = 8,
            )
        },
    )
}
