package ai.deneb.ui.chat.composables

import ai.deneb.data.Attachment
import ai.deneb.decodeToImageBitmap
import ai.deneb.ui.components.LocalShowFullScreenImage
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.scaledBy
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SuggestionChip
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_file
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import org.jetbrains.compose.resources.painterResource
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

@OptIn(ExperimentalEncodingApi::class, ExperimentalLayoutApi::class)
@Composable
internal fun UserMessage(
    message: String,
    attachments: ImmutableList<Attachment> = persistentListOf(),
    textScale: Float = 1f,
) {
    val showFullScreen = LocalShowFullScreenImage.current
    val haptics = rememberHaptics()
    // Borderless gray bubble for "my message": a neutral surfaceVariant container in
    // both light (#E1E7EE) and OLED dark (#2A2F35) — no accent wash, no hairline ring.
    val cs = MaterialTheme.colorScheme
    val bubbleShape = RoundedCornerShape(18.dp, 18.dp, 4.dp, 18.dp)
    val bubbleColor = cs.surfaceVariant
    val bubbleText = cs.onSurfaceVariant
    SelectionContainer {
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
                        // Messenger-tight padding so the bubble hugs the text instead of
                        // ballooning around it — horizontal a touch more than vertical
                        // (was a roomy uniform 16dp, which read oversized for the font).
                        .padding(horizontal = 14.dp, vertical = 9.dp),
                    horizontalAlignment = Alignment.End,
                ) {
                    val images = attachments.filter { it.mimeType.startsWith("image/") }
                    val others = attachments.filter { !it.mimeType.startsWith("image/") }
                    for (att in images) {
                        val imageBitmap = remember(att.data) {
                            try {
                                decodeToImageBitmap(Base64.decode(att.data))
                            } catch (_: Exception) {
                                null
                            }
                        }
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
                                        showFullScreen(imageBitmap)
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
                        // Explicit body style (was unstyled => LocalTextStyle default,
                        // a different size/face than the assistant's bodyLarge). 14sp
                        // keeps it one step down, matching the chat answer body.
                        Text(
                            text = message,
                            // Scaled with the assistant body so 챗봇 mode enlarges both sides.
                            style = MaterialTheme.typography.bodyMedium.scaledBy(textScale),
                            color = bubbleText,
                        )
                    }
                }
            }
        }
    }
}
