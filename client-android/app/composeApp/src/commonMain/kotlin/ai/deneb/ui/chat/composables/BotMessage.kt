package ai.deneb.ui.chat.composables

import ai.deneb.data.Attachment
import ai.deneb.getBackgroundDispatcher
import ai.deneb.ui.DenebMotion
import ai.deneb.ui.components.LocalShowFullScreenImage
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.LocalDenebUiStreaming
import ai.deneb.ui.markdown.MarkdownContent
import ai.deneb.ui.markdown.MarkdownDocument
import ai.deneb.ui.markdown.parseMarkdown
import ai.deneb.ui.markdown.parseMarkdownCached
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.bot_message_regenerate_content_description
import deneb.composeapp.generated.resources.bot_message_speech_content_description
import deneb.composeapp.generated.resources.bot_message_thinking_expand_content_description
import deneb.composeapp.generated.resources.bot_message_thinking_label
import deneb.composeapp.generated.resources.ic_refresh
import deneb.composeapp.generated.resources.ic_stop
import deneb.composeapp.generated.resources.ic_volume_up
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import nl.marc_apps.tts.TextToSpeechInstance
import nl.marc_apps.tts.errors.TextToSpeechSynthesisInterruptedError
import org.jetbrains.compose.resources.stringResource

// During streaming the answer grows by a token every few ms, and parseMarkdown
// re-parses the whole string each time — O(n²) over a long answer. Two things tame it:
// this fixed-cadence sampling decouples the parse count from the token rate (the markdown
// reflows ~10x/second, not per token), and rememberMessageDocument runs each parse OFF the
// UI thread, so neither the stream nor scrolling janks. Tuned toward smoothness over
// liveness — a slightly chunkier reflow keeps more frame headroom. The finished
// (non-streaming) string is always parsed exactly, so the completed message is
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

// rememberMessageDocument turns a (possibly streaming) body into its parsed document while
// keeping the parse off the UI thread. A finished body is a synchronous cache hit (the
// history precompute warms it, so no on-frame parse). A streaming body re-parses on each
// sampled tick — but on Dispatchers.Default (a spare core) via produceState: the previous
// document stays visible until the new parse lands (no flicker), and a tick superseded
// before its parse finishes is cancelled (implicit coalescing). Only the cheap finished-body
// cache touch runs on the UI thread.
@Composable
private fun rememberMessageDocument(source: String, isStreaming: Boolean): MarkdownDocument {
    if (!isStreaming) {
        return remember(source) { parseMarkdownCached(source) }
    }
    val empty = remember { parseMarkdown("") }
    val document by produceState(initialValue = empty, source) {
        value = withContext(Dispatchers.Default) { parseMarkdown(source) }
    }
    return document
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
    textScale: Float = 1f,
) {
    val haptics = rememberHaptics()
    val parseSource = rememberStreamingParseSource(message, isStreaming)
    val document = rememberMessageDocument(parseSource, isStreaming)
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
                    // Streaming flag lets an unclosed deneb-ui fence render as a quiet
                    // placeholder instead of a half-built form morphing per token.
                    CompositionLocalProvider(LocalDenebUiStreaming provides isStreaming) {
                        MarkdownContent(
                            document = document,
                            isInteractive = effectiveInteractive,
                            onUiCallback = denebUiCallback,
                            frozen = effectiveFrozen,
                            textScale = textScale,
                            modifier = Modifier.fillMaxWidth()
                                .padding(start = 16.dp, top = answerTopPadding, end = 16.dp, bottom = 8.dp),
                        )
                    }
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
                    // Decoded on a background core + module-cached, so it neither janks the
                    // scroll frame nor re-decodes when scrolled back into view.
                    val imageBitmap = rememberDecodedImage(att.data)
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
                                .clickable(onClickLabel = "확대") {
                                    haptics.tap()
                                    showFullScreen(imageBitmap, decodeBase64BytesOrNull(att.data))
                                },
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
                    .clickable {
                        haptics.toggle(!isEditing)
                        isEditing = !isEditing
                    },
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
    // Body text is selectable (SelectionContainer above), so there is no copy
    // button — long-press covers it. Without TTS or regenerate the meta row
    // has nothing to offer; skip it entirely.
    if (message.isEmpty() || (textToSpeech == null && onRegenerate == null)) return
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
                .clickable {
                    haptics.toggle(!expanded)
                    expanded = !expanded
                }
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
