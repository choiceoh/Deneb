@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.ChatUiState
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.unit.dp
import nl.marc_apps.tts.TextToSpeechInstance
import org.jetbrains.compose.resources.stringResource

/**
 * Immersive top overlay: the bar + banners float over the conversation (which
 * scrolls full-height behind them, under the transparent status bar). Split out
 * of ChatModeScreen.kt.
 */
// How far a floating chat bar's scrim takes to dissolve into the conversation.
// Shared by the top bar and the input bar so both edges read as the same material.
// Absolute, not a fraction of the bar: the top overlay grows when a banner shows,
// and a proportional tail would drag the controls back over transparent ground.
internal val ChatBarScrimFade = 20.dp

@Composable
internal fun ChatTopOverlay(
    uiState: ChatUiState,
    textToSpeech: TextToSpeechInstance?,
    navigationTabBar: (@Composable () -> Unit)?,
    onOpenDrawer: () -> Unit,
    modifier: Modifier = Modifier,
    onHeightChange: (Int) -> Unit,
) {
    // Immersive top overlay: the bar + banners float over the conversation
    // (which scrolls full-height behind them, under the transparent status
    // bar). The vertical scrim keeps the bar's controls legible over
    // scrolling messages; statusBarsPadding clears the OS status bar; the
    // measured height drives the message list's top contentPadding above.
    val scrim = MaterialTheme.colorScheme.background
    Column(
        modifier = modifier
            .fillMaxWidth()
            .onSizeChanged { onHeightChange(it.height) }
            .drawBehind {
                // Solid under the controls, fading out only over the last
                // [ChatBarScrimFade]. A linear ramp across the whole overlay was
                // already half transparent by the row where the hamburger and +
                // actually sit, so live message text read straight through the
                // glyphs. The tail is an absolute length, not a fraction, so a
                // banner appearing (which grows this Column) can't slide the
                // controls back into the transparent part.
                drawRect(
                    Brush.verticalGradient(
                        listOf(scrim, Color.Transparent),
                        startY = size.height - ChatBarScrimFade.toPx(),
                        endY = size.height,
                    ),
                )
            }
            .statusBarsPadding(),
    ) {
        TopBar(
            textToSpeech = textToSpeech,
            isSpeechOutputEnabled = uiState.isSpeechOutputEnabled,
            isSpeaking = uiState.isSpeaking,
            actions = uiState.actions,
            isChatHistoryEmpty = uiState.history.isEmpty(),
            // The hamburger opens the session history (left drawer).
            onOpenDrawer = onOpenDrawer,
            navigationTabBar = navigationTabBar,
            // The desktop session button (right drawer) is gone — the
            // hamburger above is the only way into sessions now.
            onOpenSessionDrawer = null,
        )

        HeartbeatBanner(
            visible = uiState.hasUnreadHeartbeat,
            onTap = {
                uiState.heartbeatConversationId?.let { uiState.actions.loadConversation(it) }
                uiState.actions.clearUnreadHeartbeat()
            },
            onDismiss = {
                uiState.actions.clearUnreadHeartbeat()
            },
        )

        WorkReportBanner(
            visible = uiState.hasUnreadWorkReport,
            onTap = {
                uiState.actions.openWorkReport()
            },
            onDismiss = {
                uiState.actions.clearUnreadWorkReport()
            },
        )

        PendingSmsBanners(
            drafts = uiState.smsDrafts,
            onSend = uiState.actions.sendSmsDraft,
            onDiscard = uiState.actions.discardSmsDraft,
        )

        uiState.warning?.let { warning ->
            Text(
                text = stringResource(warning),
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
    }
}
