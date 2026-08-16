@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat

import ai.deneb.data.AppSettings
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import nl.marc_apps.tts.TextToSpeechInstance
import org.koin.compose.koinInject
import org.koin.compose.viewmodel.koinViewModel

@Composable
fun ChatScreen(
    viewModel: ChatViewModel = koinViewModel(),
    textToSpeech: TextToSpeechInstance?,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val uiState by viewModel.state.collectAsStateWithLifecycle()
    val appSettings = koinInject<AppSettings>()

    ChatScreenContent(
        uiState = uiState,
        textToSpeech = textToSpeech,
        navigationTabBar = navigationTabBar,
        initialDraft = appSettings.getComposerDraft(),
        onDraftChange = { appSettings.setComposerDraft(it) },
    )
}

@Composable
fun ChatScreenContent(
    uiState: ChatUiState,
    textToSpeech: TextToSpeechInstance? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
    initialDraft: String = "",
    onDraftChange: (String) -> Unit = {},
) {
    ChatModeScreen(
        uiState = uiState,
        textToSpeech = textToSpeech,
        navigationTabBar = navigationTabBar,
        initialDraft = initialDraft,
        onDraftChange = onDraftChange,
    )
}
