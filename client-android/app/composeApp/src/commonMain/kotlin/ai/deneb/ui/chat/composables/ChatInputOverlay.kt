@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.ChatUiState
import ai.deneb.ui.denebContentWidthModifier
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * Composer sibling under the message list. Split out of ChatModeScreen.kt.
 * The list+composer column shrinks together with IME, so the input rides the
 * keyboard without a follow-scroll or overlay contentPadding.
 */
@Composable
internal fun ChatInputOverlay(
    uiState: ChatUiState,
    questionInputText: TextFieldValue,
    onQuestionInputTextChange: (TextFieldValue) -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.background),
        contentAlignment = TopCenter,
    ) {
        Column(denebContentWidthModifier()) {
            // Messages queued while the reply streams (sent with the
            // queue-send button): a quiet one-line notice above the
            // input — first message previewed, +N for the rest, × drops
            // the queue. They fire automatically when the turn completes.
            val lastSteer = uiState.lastSteerNote
            if (!lastSteer.isNullOrBlank()) {
                Text(
                    text = "끼어들기 전달됨: $lastSteer",
                    style = DenebType.snippet,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 28.dp, vertical = 2.dp),
                )
            }
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
