@file:OptIn(
    ExperimentalFoundationApi::class,
    ExperimentalMaterial3Api::class,
)

package ai.deneb.ui.chat

import ai.deneb.BackIcon
import ai.deneb.data.supportsAgenticFlows
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.composables.CircleIconButton
import ai.deneb.ui.chat.composables.ErrorMessage
import ai.deneb.ui.chat.composables.QuestionInput
import ai.deneb.ui.chat.composables.ServiceSelector
import ai.deneb.ui.chat.composables.TrailingIcon
import ai.deneb.ui.chat.composables.WaitingResponseRow
import ai.deneb.ui.components.LogoAnimation
import ai.deneb.ui.components.animatedGradientBorder
import ai.deneb.ui.dynamicui.DenebUiParser
import ai.deneb.ui.dynamicui.DenebUiRenderer
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.DenebUiBlock
import ai.deneb.ui.markdown.parseMarkdown
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Alignment.Companion.BottomCenter
import androidx.compose.ui.Alignment.Companion.CenterHorizontally
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_stop
import deneb.composeapp.generated.resources.interactive_back_content_description
import deneb.composeapp.generated.resources.interactive_exit_content_description
import deneb.composeapp.generated.resources.interactive_title
import deneb.composeapp.generated.resources.interactive_ui_parsing_failed
import deneb.composeapp.generated.resources.interactive_welcome_subtitle
import deneb.composeapp.generated.resources.interactive_welcome_title
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import org.jetbrains.compose.resources.stringResource

/**
 * Interactive mode of the chat surface: a full-screen deneb-ui document with its
 * own top bar, rendered when the agent pushes an interactive screen. Split out of
 * ChatScreen.kt so the mode can grow without re-bloating the entry file.
 */
@Composable
internal fun InteractiveModeScreen(uiState: ChatUiState) {
    val snackbarHostState = remember { SnackbarHostState() }

    // Intercept system back to exit interactive mode instead of closing the app
    ai.deneb.PlatformBackHandler(enabled = true) {
        uiState.actions.exitInteractiveMode()
    }

    val hasAssistantResponse = remember(uiState.history) {
        uiState.history.any { it.role == History.Role.ASSISTANT }
    }
    // Interactive mode drives a tool-calling loop and emits deneb-ui JSON, so the
    // switcher only lists services/models capable of agentic flows.
    val interactiveServices = remember(uiState.availableServices) {
        uiState.availableServices
            .filter { supportsAgenticFlows(it.serviceId, it.modelId) }
            .toImmutableList()
    }
    var inputExpanded by remember { mutableStateOf(true) }
    LaunchedEffect(hasAssistantResponse, uiState.history.size) {
        if (hasAssistantResponse) inputExpanded = false
    }
    val showFullInput = inputExpanded && !uiState.isLoading
    var questionInputText by rememberSaveable(stateSaver = TextFieldValue.Saver) {
        mutableStateOf(TextFieldValue(""))
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .navigationBarsPadding()
            .statusBarsPadding()
            .imePadding(),
    ) {
        Column(Modifier.fillMaxSize()) {
            // Top bar with back and close
            InteractiveModeTopBar(
                onBack = uiState.actions.goBackInteractiveMode,
                onExit = uiState.actions.exitInteractiveMode,
                isLoading = uiState.isLoading,
                showBack = hasAssistantResponse,
            )

            Box(
                modifier = Modifier.fillMaxSize(),
            ) {
                // Content area fills remaining space
                if (!hasAssistantResponse && !uiState.isLoading) {
                    Column(
                        modifier = Modifier.align(Alignment.Center),
                        horizontalAlignment = CenterHorizontally,
                        verticalArrangement = Arrangement.Center,
                    ) {
                        LogoAnimation()
                        Spacer(Modifier.height(16.dp))
                        Text(
                            text = stringResource(Res.string.interactive_welcome_title),
                            style = DenebType.subject,
                            color = MaterialTheme.colorScheme.onBackground,
                        )
                        Text(
                            text = stringResource(Res.string.interactive_welcome_subtitle),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 4.dp),
                        )
                    }
                } else {
                    SelectionContainer {
                        InteractiveModeContent(
                            uiState = uiState,
                            modifier = Modifier.fillMaxSize(),
                            bottomPadding = 88.dp,
                        )
                    }
                }

                // Full QuestionInput stays in the column flow
                if (showFullInput) {
                    QuestionInput(
                        modifier = Modifier.align(Alignment.BottomEnd),
                        files = uiState.files,
                        addFile = uiState.actions.addFile,
                        removeFile = uiState.actions.removeFile,
                        ask = {
                            inputExpanded = false
                            uiState.actions.ask(it)
                        },
                        supportedFileExtensions = uiState.supportedFileExtensions,
                        textState = questionInputText,
                        onTextStateChange = { questionInputText = it },
                        isLoading = uiState.isLoading,
                        cancel = uiState.actions.cancel,
                        availableServices = interactiveServices,
                        onSelectService = uiState.actions.selectService,
                    )
                }
            }
        }

        // Collapsed pill floats over content at the bottom-end
        if (!showFullInput) {
            Row(
                modifier = Modifier
                    .align(Alignment.BottomEnd)
                    .padding(16.dp)
                    .height(56.dp)
                    .clip(RoundedCornerShape(28.dp))
                    .background(MaterialTheme.colorScheme.surfaceContainer, RoundedCornerShape(28.dp))
                    .animatedGradientBorder(cornerRadius = 28.dp, borderWidth = 2.dp)
                    .handCursor()
                    .padding(horizontal = 7.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                if (uiState.isLoading) {
                    TrailingIcon(
                        icon = Res.drawable.ic_stop,
                        onClick = { uiState.actions.cancel() },
                        isPulsing = true,
                    )
                } else {
                    CircleIconButton(
                        icon = Icons.Default.Edit,
                        onClick = { inputExpanded = true },
                    )
                }
                if (interactiveServices.size > 1) {
                    ServiceSelector(
                        services = interactiveServices,
                        onSelectService = uiState.actions.selectService,
                    )
                }
            }
        }

        SnackbarHost(
            hostState = snackbarHostState,
            modifier = Modifier.align(BottomCenter).padding(bottom = 80.dp),
        ) { data ->
            Snackbar(snackbarData = data)
        }
    }
}

@Composable
private fun InteractiveModeTopBar(
    onBack: () -> Unit,
    onExit: () -> Unit,
    isLoading: Boolean,
    showBack: Boolean,
) {
    val iconColor = MaterialTheme.colorScheme.onSurface

    Row(
        modifier = Modifier
            .fillMaxWidth(),
        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
    ) {
        if (showBack) {
            IconButton(
                onClick = onBack,
                enabled = !isLoading,
                modifier = Modifier.handCursor(),
            ) {
                Icon(
                    BackIcon,
                    contentDescription = stringResource(Res.string.interactive_back_content_description),
                    tint = iconColor,
                )
            }
        } else {
            // Placeholder to keep close button aligned right
            Spacer(Modifier.size(48.dp))
        }
        Spacer(Modifier.weight(1f))
        Text(
            text = stringResource(Res.string.interactive_title),
            style = DenebType.cardTitle,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(Modifier.weight(1f))
        IconButton(
            onClick = onExit,
            modifier = Modifier.handCursor(),
        ) {
            Icon(
                Icons.Default.Close,
                contentDescription = stringResource(Res.string.interactive_exit_content_description),
                tint = iconColor,
            )
        }
    }
}

@Composable
private fun InteractiveModeContent(
    uiState: ChatUiState,
    modifier: Modifier = Modifier,
    bottomPadding: androidx.compose.ui.unit.Dp = 0.dp,
) {
    val lastAssistant = remember(uiState.history) { uiState.history.lastRenderedAssistant() }

    Box(modifier.fillMaxWidth()) {
        if (uiState.isLoading && lastAssistant == null) {
            // First load — show centered loading
            Box(Modifier.fillMaxSize(), contentAlignment = androidx.compose.ui.Alignment.Center) {
                WaitingResponseRow(
                    executingTools = remember { kotlinx.collections.immutable.persistentListOf() },
                )
            }
        } else if (lastAssistant != null) {
            val contentId = lastAssistant.id
            AnimatedContent(
                targetState = contentId,
                transitionSpec = { fadeIn() togetherWith fadeOut() },
                modifier = Modifier.fillMaxSize(),
            ) { _ ->
                val blocks = remember(lastAssistant.content) { parseMarkdown(DenebUiParser.wrapBareDenebUiContent(lastAssistant.content)).blocks }
                val uiBlocks = blocks.filterIsInstance<DenebUiBlock>()

                if (uiBlocks.isNotEmpty()) {
                    Column(
                        modifier = Modifier
                            .fillMaxSize()
                            .verticalScroll(rememberScrollState())
                            .padding(start = 12.dp, end = 12.dp, top = 8.dp, bottom = 8.dp + bottomPadding),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        for (block in uiBlocks) {
                            DenebUiRenderer(
                                node = block.node,
                                isInteractive = !uiState.isLoading,
                                onCallback = { event, data ->
                                    uiState.actions.submitUiCallback(event, data)
                                },
                                wrapInCard = false,
                            )
                        }
                    }
                } else if (uiState.error == null) {
                    // AI responded with no valid deneb-ui AND there's no API error underneath —
                    // this is a genuine parse failure (retries exhausted). When an API error is
                    // set, the ErrorMessage overlay below takes over with the correct message.
                    Box(Modifier.fillMaxSize(), contentAlignment = androidx.compose.ui.Alignment.Center) {
                        Column(horizontalAlignment = CenterHorizontally) {
                            Text(
                                text = stringResource(Res.string.interactive_ui_parsing_failed),
                                style = MaterialTheme.typography.bodyLarge,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }

        // Error state
        uiState.error?.let { error ->
            Box(Modifier.fillMaxSize(), contentAlignment = androidx.compose.ui.Alignment.Center) {
                ErrorMessage(error = error, retry = uiState.actions.retry)
            }
        }
    }
}
