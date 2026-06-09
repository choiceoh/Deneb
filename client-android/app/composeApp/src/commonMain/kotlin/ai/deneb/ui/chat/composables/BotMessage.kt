package ai.deneb.ui.chat.composables

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.layout.ContentScale
import ai.deneb.data.Attachment
import ai.deneb.ui.components.LocalShowFullScreenImage
import ai.deneb.ui.components.rememberHaptics
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.getBackgroundDispatcher
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.DenebMotion
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.MarkdownContent
import ai.deneb.ui.markdown.parseMarkdown
import ai.deneb.ui.markdown.parseMarkdownCached
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.bot_message_copy_content_description
import deneb.composeapp.generated.resources.bot_message_regenerate_content_description
import deneb.composeapp.generated.resources.bot_message_speech_content_description
import deneb.composeapp.generated.resources.bot_message_thinking_expand_content_description
import deneb.composeapp.generated.resources.bot_message_thinking_label
import deneb.composeapp.generated.resources.ic_copy
import deneb.composeapp.generated.resources.ic_refresh
import deneb.composeapp.generated.resources.ic_stop
import deneb.composeapp.generated.resources.ic_volume_up
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.errors.TextToSpeechSynthesisInterruptedError
import org.jetbrains.compose.resources.stringResource

// During streaming the answer grows by a token every few ms, and parseMarkdown
// re-parses the whole string each time — O(n²) over a long answer, which stalls the
// main thread and janks both the stream and any scrolling. Sample the growing text at
// a fixed cadence instead: the markdown reflows ~10x/second while the parse count is
// decoupled from the token rate. Tuned toward smoothness over liveness — a slightly
// chunkier reflow keeps more frame headroom for scrolling. The finished (non-streaming)
// string is always parsed exactly and immediately, so the completed message is
// byte-identical to having parsed every token.
private const val STREAM_PARSE_INTERVAL_MS = 96L

@Composable
private fun rememberStreamingParseSource(message: String, isStreaming: Boolean): String {
    val latest = rememberUpdatedState(message)
    var sampled by remember { mutableStateOf(message) }
    LaunchedEffect(isStreaming) {
        // Keyed on isStreaming: when it flips to false this effect is cancelled and
        // relaunched, so the while-loop only runs for the lifetime of the stream.
        if (!isStreaming) {
            sampled = latest.value
            return@LaunchedEffect
        }
        while (true) {
            sampled = latest.value
            delay(STREAM_PARSE_INTERVAL_MS)
        }
    }
    return if (isStreaming) sampled else message
}

@Composable
internal fun BotMessage(
    message: String,
    textToSpeech: TextToSpeechInstance?,
    isSpeaking: Boolean,
    setIsSpeaking: (Boolean) -> Unit,
    onRegenerate: (() -> Unit)? = null,
    isInteractive: Boolean = false,
    onUiCallback: ((event: String, data: Map<String, String>) -> Unit)? = null,
    frozen: FrozenSubmission? = null,
    onResubmit: ((event: String, data: Map<String, String>) -> Unit)? = null,
    reasoningSegments: ImmutableList<String> = persistentListOf(),
    isStreaming: Boolean = false,
    attachments: ImmutableList<Attachment> = persistentListOf(),
) {
    val haptics = rememberHaptics()
    val parseSource = rememberStreamingParseSource(message, isStreaming)
    // Streaming bodies change every tick (don't pollute the cache); a finished body goes
    // through the module-level cache so scrolling it back into view never re-parses.
    val document = remember(parseSource, isStreaming) {
        if (isStreaming) parseMarkdown(parseSource) else parseMarkdownCached(parseSource)
    }
    var isEditing by remember(frozen) { mutableStateOf(false) }
    val effectiveFrozen = if (isEditing && frozen != null) frozen.copy(pressedEvent = null) else frozen
    val effectiveInteractive = if (frozen != null) (onResubmit != null && isEditing) else isInteractive
    val denebUiCallback: (String, Map<String, String>) -> Unit = if (onResubmit != null) {
        { event, data ->
            isEditing = false
            onResubmit(event, data)
        }
    } else {
        onUiCallback ?: { _, _ -> }
    }

    Box(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.fillMaxWidth()) {
            val nonBlankSegments = remember(reasoningSegments) {
                reasoningSegments.filter { it.isNotBlank() }.toImmutableList()
            }
            if (nonBlankSegments.isNotEmpty()) {
                ReasoningBlockquote(
                    segments = nonBlankSegments,
                    modifier = Modifier.fillMaxWidth()
                        .padding(start = 16.dp, top = 12.dp, end = 16.dp),
                )
            }
            if (message.isNotEmpty()) {
                // When reasoning is shown above, the Thinking row already provides
                // the visual gap to the answer — drop the duplicated top inset.
                val answerTopPadding = if (nonBlankSegments.isNotEmpty()) 6.dp else 16.dp
                SelectionContainer {
                    MarkdownContent(
                        document = document,
                        isInteractive = effectiveInteractive,
                        onUiCallback = denebUiCallback,
                        frozen = effectiveFrozen,
                        modifier = Modifier.fillMaxWidth()
                            .padding(start = 16.dp, top = answerTopPadding, end = 16.dp, bottom = 8.dp),
                    )
                }
            }
            // Inbound image attachments (e.g. the proactive 주간업무보고 form). Tap to
            // open full-screen. Non-image attachments are ignored here — proactive
            // reports only ship images.
            val imageAttachments = remember(attachments) {
                attachments.filter { it.mimeType.startsWith("image/") }
            }
            if (imageAttachments.isNotEmpty()) {
                val showFullScreen = LocalShowFullScreenImage.current
                for (att in imageAttachments) {
                    // Module-cached so scrolling a form/receipt back into view doesn't re-decode.
                    val imageBitmap = remember(att.data) { decodeBase64ImageCached(att.data) }
                    if (imageBitmap != null) {
                        Image(
                            bitmap = imageBitmap,
                            contentDescription = "주간업무보고 양식",
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(start = 16.dp, end = 16.dp, bottom = 8.dp)
                                .widthIn(max = 520.dp)
                                .clip(RoundedCornerShape(8.dp))
                                .handCursor()
                                .clickable(onClickLabel = "확대") { haptics.tap(); showFullScreen(imageBitmap) },
                            contentScale = ContentScale.FillWidth,
                        )
                    }
                }
            }
            if (isStreaming) {
                StreamingCaret()
            }
        }
        if (frozen != null && onResubmit != null) {
            Box(
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .padding(8.dp)
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(MaterialTheme.colorScheme.surfaceContainer)
                    .handCursor()
                    .clickable { haptics.toggle(!isEditing); isEditing = !isEditing },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = if (isEditing) Icons.Default.Close else Icons.Default.Edit,
                    contentDescription = if (isEditing) "편집 취소" else "제출 편집",
                    modifier = Modifier.size(16.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
    if (message.isEmpty()) return
    Row(Modifier.padding(horizontal = 8.dp)) {
        if (textToSpeech != null) {
            val componentScope = rememberCoroutineScope()
            SmallIconButton(
                iconResource = if (isSpeaking) Res.drawable.ic_stop else Res.drawable.ic_volume_up,
                contentDescription = stringResource(Res.string.bot_message_speech_content_description),
                onClick = {
                    componentScope.launch(getBackgroundDispatcher()) {
                        textToSpeech.stop()
                        if (isSpeaking) {
                            setIsSpeaking(false)
                        } else {
                            setIsSpeaking(true)
                            try {
                                textToSpeech.say(text = message.toSpeakableText())
                            } catch (ignore: TextToSpeechSynthesisInterruptedError) {
                                // Expected interruption - no action needed
                            } catch (e: Exception) {
                                // Handle TTS errors gracefully (service failure, audio issues, etc.)
                            }
                            setIsSpeaking(false)
                        }
                    }
                },
            )
        }
        val clipboardManager = LocalClipboardManager.current
        SmallIconButton(
            iconResource = Res.drawable.ic_copy,
            contentDescription = stringResource(Res.string.bot_message_copy_content_description),
            onClick = {
                clipboardManager.setText(buildAnnotatedString { append(message) })
            },
        )
        if (onRegenerate != null) {
            SmallIconButton(
                iconResource = Res.drawable.ic_refresh,
                contentDescription = stringResource(Res.string.bot_message_regenerate_content_description),
                onClick = onRegenerate,
            )
        }
        Spacer(Modifier.weight(1f))
    }
}

/** Blinking caret shown at the end of a reply while it streams in. */
@Composable
private fun StreamingCaret() {
    val transition = rememberInfiniteTransition(label = "caret")
    val caretAlpha by transition.animateFloat(
        initialValue = 1f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(720, easing = DenebMotion.emphasized), RepeatMode.Reverse),
        label = "caret-alpha",
    )
    // A thin rounded cursor bar reads cleaner than the "▍" glyph, which sits
    // chunky and slightly off the text baseline.
    Box(
        modifier = Modifier
            .padding(start = 16.dp, bottom = 10.dp)
            .alpha(caretAlpha)
            .size(width = 2.dp, height = 17.dp)
            .clip(RoundedCornerShape(1.dp))
            .background(MaterialTheme.colorScheme.primary),
    )
}

@Composable
private fun ReasoningBlockquote(
    segments: ImmutableList<String>,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    // Preview always reflects the MOST RECENT thinking segment so the user gets a
    // visual update each time a new reasoning phase starts, without expanding.
    val preview = remember(segments) {
        segments.lastOrNull()
            ?.lineSequence()
            ?.map { it.trim() }
            ?.firstOrNull { it.isNotEmpty() }
            .orEmpty()
    }

    Column(modifier = modifier) {
        Row(
            modifier = Modifier.fillMaxWidth()
                .clickable { haptics.toggle(!expanded); expanded = !expanded }
                .handCursor(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = if (expanded) Icons.Default.KeyboardArrowUp else Icons.Default.KeyboardArrowDown,
                contentDescription = stringResource(Res.string.bot_message_thinking_expand_content_description),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
            Spacer(Modifier.size(6.dp))
            Text(
                text = stringResource(Res.string.bot_message_thinking_label),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (!expanded && preview.isNotEmpty()) {
                Text(
                    text = " · $preview",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f).padding(start = 4.dp),
                )
            }
        }
        AnimatedVisibility(
            visible = expanded,
            enter = denebExpandIn,
            exit = denebShrinkOut,
        ) {
            Column(
                modifier = Modifier.padding(top = 6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                for (segment in segments) {
                    Row(modifier = Modifier.height(IntrinsicSize.Min)) {
                        VerticalDivider(
                            thickness = 2.dp,
                            color = MaterialTheme.colorScheme.outlineVariant,
                            modifier = Modifier.fillMaxHeight(),
                        )
                        SelectionContainer(modifier = Modifier.padding(start = 10.dp)) {
                            Text(
                                text = segment,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                    }
                }
            }
        }
    }
}
