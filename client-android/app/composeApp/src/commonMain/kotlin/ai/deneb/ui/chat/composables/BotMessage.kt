package ai.deneb.ui.chat.composables

import ai.deneb.data.Attachment
import ai.deneb.getBackgroundDispatcher
import ai.deneb.shareTextToApps
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.LocalShowFullScreenImage
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebFadeEnter
import ai.deneb.ui.denebFadeExit
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.denebSnappySpring
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.toSpeakableText
import ai.deneb.ui.handCursor
import ai.deneb.ui.icons.filled.ChevronLeft
import ai.deneb.ui.icons.filled.ChevronRight
import ai.deneb.ui.icons.filled.ContentCopy
import ai.deneb.ui.icons.filled.MoreHoriz
import ai.deneb.ui.markdown.LocalDenebUiStreaming
import ai.deneb.ui.markdown.MarkdownContent
import ai.deneb.ui.markdown.MarkdownDocument
import ai.deneb.ui.markdown.parseMarkdown
import ai.deneb.ui.markdown.parseMarkdownCached
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateContentSize
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
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.Share
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.bot_message_reasoning_label
import deneb.composeapp.generated.resources.bot_message_reasoning_steps
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

// Ink inset a Material chevron carries inside its own 16dp box. Measured, not
// guessed: at a 2x density the header's leftmost ink sat at 20.0dp against the
// reasoning text's 17.0dp.
private val ChevronOpticalInset = 3.dp

// The streaming caret is NOT in this string. It used to be — a "▍" appended to the
// parse source — which put a glyph of ours through the markdown parser and needed
// fence-tracking to keep it from re-opening a just-closed code block. It is now drawn
// from the text layout at the document's frontier (MarkdownContent(streamingCaret),
// StreamCaret.kt), so the parse source is exactly what the model sent.
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
    if (!isStreaming) return message
    return sampled
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
    /**
     * False for the intermediate fragments of one answer. A response arrives as
     * several assistant messages (text → tool call → more text), and drawing the
     * 복사/⋯ row under each one repeated the same controls three or four times in
     * a single turn. Only the response's last fragment shows the row.
     */
    showActions: Boolean = true,
    /**
     * What 복사 puts on the clipboard. Null = this message alone; the list passes
     * the WHOLE response so copying from the tail fragment does not silently hand
     * over just the last paragraph.
     */
    copyText: String? = null,
    isInteractive: Boolean = false,
    onUiCallback: ((event: String, data: Map<String, String>) -> Unit)? = null,
    frozen: FrozenSubmission? = null,
    onResubmit: ((event: String, data: Map<String, String>) -> Unit)? = null,
    reasoningSegments: ImmutableList<String> = persistentListOf(),
    isStreaming: Boolean = false,
    attachments: ImmutableList<Attachment> = persistentListOf(),
    timestampMs: Long = 0,
    variantNav: VariantNav? = null,
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
                    isStreaming = isStreaming,
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
                            streamingCaret = isStreaming,
                            modifier = Modifier.fillMaxWidth()
                                // While streaming, glide the height between sampled
                                // reflows instead of jumping a chunk at a time — the
                                // single biggest "flows vs stutters" cue. Settled rows
                                // skip the modifier entirely (no layout-animation cost
                                // on history, and no size animation on transcript load).
                                .then(
                                    if (isStreaming) {
                                        Modifier.animateContentSize(animationSpec = denebSnappySpring())
                                    } else {
                                        Modifier
                                    },
                                )
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
    // Meta row under every non-empty reply: copy (always), then TTS/regenerate when
    // available. Copy puts the raw markdown SOURCE on the clipboard — not the rendered
    // AnnotatedString that long-press/SelectionContainer yields (which loses table
    // structure and leaks inline-code hair spaces) — so it round-trips into any
    // markdown-aware destination (tables stay `| a | b |`, code stays verbatim).
    if (message.isEmpty()) return
    val clipboard = LocalClipboardManager.current
    var copied by remember(message) { mutableStateOf(false) }
    // ⋯ 시트 상태/스코프는 여기(함수 수명)에 — 시트가 닫히며 액션이 실행되므로,
    // if-블록 안의 스코프면 dismiss 리컴포지션이 공유 코루틴을 취소한다.
    var moreSheetOpen by remember { mutableStateOf(false) }
    val moreScope = rememberCoroutineScope()
    LaunchedEffect(copied) {
        if (copied) {
            delay(1500)
            copied = false
        }
    }
    // Actions reveal when the answer settles: a copy/regenerate row under a
    // half-written reply invites acting on incomplete content. History rows
    // compose with visible=true, so only the live streaming→done transition
    // animates.
    AnimatedVisibility(visible = showActions && !isStreaming, enter = denebFadeEnter, exit = denebFadeExit) {
        // Optically flush with the answer's text column, not mathematically: the
        // answer starts at 16dp, and SmallIconButton centres a 14dp glyph in a 36dp
        // hit box, so its ink already sits 11dp in. 8dp here put the icons 3.5dp
        // inside the prose — invisible at 1x, a clear step at a phone's real density.
        Row(Modifier.padding(start = 5.dp, end = 8.dp)) {
            SmallIconButton(
                imageVector = if (copied) Icons.Filled.Check else Icons.Filled.ContentCopy,
                contentDescription = if (copied) "복사됨" else "복사",
                onClick = {
                    clipboard.setText(AnnotatedString(copyText ?: message))
                    copied = true
                },
            )
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
            // ‹ n/N › — navigate the last answer's regenerate variants. Arrows
            // only render where a step exists (no disabled-state ambiguity at
            // this glyph size); the fraction reads current/total.
            if (variantNav != null) {
                if (variantNav.index > 0) {
                    SmallIconButton(
                        imageVector = Icons.Filled.ChevronLeft,
                        contentDescription = "이전 답변",
                        onClick = { variantNav.onSelect(variantNav.index - 1) },
                    )
                }
                Text(
                    text = "${variantNav.index + 1}/${variantNav.total}",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.align(Alignment.CenterVertically),
                )
                if (variantNav.index < variantNav.total - 1) {
                    SmallIconButton(
                        imageVector = Icons.Filled.ChevronRight,
                        contentDescription = "다음 답변",
                        onClick = { variantNav.onSelect(variantNav.index + 1) },
                    )
                }
            }
            // ⋯ — the message action sheet (공유·보낸 시각). Bot bodies keep their
            // inline SelectionContainer (drag-select works there), so the sheet rides
            // an explicit button instead of long-press.
            SmallIconButton(
                imageVector = Icons.Filled.MoreHoriz,
                contentDescription = "메시지 더보기",
                onClick = { moreSheetOpen = true },
            )
            Spacer(Modifier.weight(1f))
        }
    }
    if (moreSheetOpen) {
        MessageActionsSheet(
            timestampMs = timestampMs,
            actions = listOf(
                MessageSheetAction(Icons.Filled.Share, "공유") { moreScope.launch { shareTextToApps(message) } },
            ),
            onDismiss = { moreSheetOpen = false },
        )
    }
}

/**
 * The live-progress line for a streaming reasoning block: the first non-empty
 * line of the MOST RECENT segment, so the row visibly moves each time a new
 * reasoning phase starts without the user expanding anything.
 *
 * Only used while the turn is running. A finished turn shows its step count
 * instead — see [ReasoningBlockquote].
 */
internal fun reasoningPreviewLine(segments: List<String>): String = segments.lastOrNull()
    ?.lineSequence()
    ?.map { it.trim() }
    ?.firstOrNull { it.isNotEmpty() }
    .orEmpty()

@Composable
private fun ReasoningBlockquote(
    segments: ImmutableList<String>,
    isStreaming: Boolean,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    // WHILE STREAMING the tail of the current segment is live progress — it tells
    // the user the turn is moving and roughly where. ONCE THE TURN IS DONE that
    // same line is a frozen fragment of raw model reasoning: it is written in the
    // model's own language (English, in a Korean-first product), cut mid-sentence,
    // and answers nothing the finished answer above does not. So a finished turn
    // shows the SHAPE of the reasoning (how many phases) and keeps the words
    // behind the expand toggle.
    val preview = remember(segments) { reasoningPreviewLine(segments) }
    // "생각 중" on a turn that finished minutes ago is simply false. The label
    // tracks the turn: present-progressive while running, a noun once it is a
    // record.
    val label = if (isStreaming) {
        stringResource(Res.string.bot_message_thinking_label)
    } else {
        stringResource(Res.string.bot_message_reasoning_label)
    }
    val trailing = if (isStreaming) {
        preview
    } else {
        stringResource(Res.string.bot_message_reasoning_steps, segments.size)
    }

    Column(modifier = modifier) {
        Row(
            // Same optical-alignment story as the action row: the Material chevron
            // carries its own padding inside a 16dp box, so at the column's
            // 16dp the header read as indented from the reasoning text below it.
            // Shifting the whole row keeps the chevron-to-label gap intact, and
            // fillMaxWidth means it ends 4dp early rather than overhanging.
            modifier = Modifier.fillMaxWidth()
                .offset(x = -ChevronOpticalInset)
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
                text = label,
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (!expanded && trailing.isNotEmpty()) {
                Text(
                    text = " · $trailing",
                    style = DenebType.snippet,
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
                                style = DenebType.snippet,
                            )
                        }
                    }
                }
            }
        }
    }
}

// ‹ n/N › navigation over the last answer's regenerate variants
// (ChatUiState.lastAnswerVariants). index is 0-based; total-1 == the live answer.
internal data class VariantNav(
    val index: Int,
    val total: Int,
    val onSelect: (Int) -> Unit,
)
