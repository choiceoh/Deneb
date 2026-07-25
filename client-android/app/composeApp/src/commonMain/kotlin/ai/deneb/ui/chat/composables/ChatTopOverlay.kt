@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.ChatUiState
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
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
    Column(
        modifier = modifier
            .fillMaxWidth()
            .onSizeChanged { onHeightChange(it.height) }
            .background(
                Brush.verticalGradient(
                    0f to MaterialTheme.colorScheme.background,
                    1f to Color.Transparent,
                ),
            )
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
