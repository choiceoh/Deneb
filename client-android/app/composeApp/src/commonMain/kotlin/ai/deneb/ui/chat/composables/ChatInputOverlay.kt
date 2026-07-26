@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.ChatUiState
import ai.deneb.ui.denebContentWidthModifier
import ai.deneb.ui.handCursor
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment.Companion.Center
import androidx.compose.ui.Alignment.Companion.CenterVertically
import androidx.compose.ui.Alignment.Companion.TopCenter
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * Immersive bottom: the input bar floats over the conversation (which scrolls
 * under it). Split out of ChatModeScreen.kt.
 */
@Composable
internal fun ChatInputOverlay(
    uiState: ChatUiState,
    questionInputText: TextFieldValue,
    onQuestionInputTextChange: (TextFieldValue) -> Unit,
    modifier: Modifier = Modifier,
    onHeightChange: (Int) -> Unit,
) {
    // Immersive bottom: the input bar floats over the conversation (which
    // scrolls under it). A bottom-up scrim keeps it legible over scrolling
    // messages; the measured height feeds the list's bottom contentPadding so the
    // last message rests just above the input. The nav-bar + ime insets
    // come from the root Box, so the input still sits above the gesture bar
    // and rises with the keyboard. Same width cap as the message rows so it
    // lines up with the conversation column on desktop.
    val scrim = MaterialTheme.colorScheme.background
    Box(
        modifier = modifier
            .fillMaxWidth()
            .onSizeChanged { onHeightChange(it.height) }
            .drawBehind {
                // Mirror of the top bar: the fade is the first [ChatBarScrimFade]
                // and everything below it is solid. The old full-height ramp was
                // still ~7% short of opaque at the very bottom edge, which left
                // half a line of transcript ghosting through the strip between
                // the input field and the tab bar.
                drawRect(
                    Brush.verticalGradient(
                        listOf(Color.Transparent, scrim),
                        startY = 0f,
                        endY = ChatBarScrimFade.toPx(),
                    ),
                )
            },
        contentAlignment = TopCenter,
    ) {
        Column(denebContentWidthModifier()) {
            // Messages queued while the reply streams (sent with the
            // queue-send button): a quiet one-line notice above the
            // input — first message previewed, +N for the rest, × drops
            // the queue. They fire automatically when the turn completes.
            val pending = uiState.pendingQuestions
            if (pending.isNotEmpty()) {
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 28.dp, vertical = 2.dp),
                    verticalAlignment = CenterVertically,
                ) {
                    Text(
                        text = "답변 후 전송: " + pending.first().text +
                            (if (pending.size > 1) "  외 ${pending.size - 1}건" else ""),
                        style = DenebType.snippet,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    // Text-first cancel with real control semantics: a 44dp
                    // box gives an adequate touch target + Role.Button /
                    // TalkBack label (same pattern as the session drawer's
                    // × delete affordance) without a full Material button
                    // inflating this quiet one-line notice.
                    Box(
                        modifier = Modifier
                            .padding(start = 4.dp)
                            .size(44.dp)
                            .clickable(
                                onClickLabel = "대기열 취소",
                                role = Role.Button,
                                onClick = uiState.actions.cancelPendingQuestions,
                            )
                            .handCursor(),
                        contentAlignment = Center,
                    ) {
                        Text(
                            text = "취소",
                            style = DenebType.meta,
                            color = MaterialTheme.colorScheme.primary,
                        )
                    }
                }
            }
            QuestionInput(
                files = uiState.files,
                addFile = uiState.actions.addFile,
                removeFile = uiState.actions.removeFile,
                ask = uiState.actions.ask,
                supportedFileExtensions = uiState.supportedFileExtensions,
                textState = questionInputText,
                onTextStateChange = onQuestionInputTextChange,
                isLoading = uiState.isLoading,
                cancel = uiState.actions.cancel,
                availableServices = uiState.availableServices,
                onSelectService = uiState.actions.selectService,
            )
        }
    }
}
