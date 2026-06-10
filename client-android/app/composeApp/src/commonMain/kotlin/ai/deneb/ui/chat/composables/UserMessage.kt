package ai.deneb.ui.chat.composables

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import ai.deneb.data.Attachment
import ai.deneb.decodeToImageBitmap
import ai.deneb.ui.components.LocalShowFullScreenImage
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
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
) {
    val showFullScreen = LocalShowFullScreenImage.current
    val haptics = rememberHaptics()
    // Dark/OLED: an aurora-tinted bubble (primary wash + hairline ring) so "my
    // message" carries the brand accent against the flat black surface. Light
    // keeps the solid secondaryContainer — a low-alpha wash was nearly invisible
    // on the white background (the lesson that picked secondaryContainer).
    val cs = MaterialTheme.colorScheme
    val dark = cs.background.luminance() < 0.5f
    val bubbleShape = RoundedCornerShape(18.dp, 18.dp, 4.dp, 18.dp)
    val bubbleColor = if (dark) cs.primary.copy(alpha = 0.16f) else cs.secondaryContainer
    val bubbleText = if (dark) Color(0xFFEAF1F8) else cs.onSecondaryContainer
    SelectionContainer {
        Row(Modifier.padding(16.dp)) {
            Spacer(Modifier.weight(1f))
            Column(
                modifier = Modifier
                    .background(bubbleColor, bubbleShape)
                    .then(
                        if (dark) {
                            Modifier.border(1.dp, cs.primary.copy(alpha = 0.28f), bubbleShape)
                        } else {
                            Modifier
                        },
                    )
                    .padding(16.dp),
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
                                .clickable(onClickLabel = "확대") { haptics.tap(); showFullScreen(imageBitmap) },
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
                        style = MaterialTheme.typography.bodyMedium,
                        color = bubbleText,
                    )
                }
            }
        }
    }
}
