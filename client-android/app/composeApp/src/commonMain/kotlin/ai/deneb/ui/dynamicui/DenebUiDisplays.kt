@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebOnSuccessContainer
import ai.deneb.ui.denebOnWarningContainer
import ai.deneb.ui.denebSuccessContainer
import ai.deneb.ui.denebWarningContainer
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Image
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlin.time.Clock
import kotlin.time.Duration.Companion.seconds

/**
 * Read-only display components of the deneb-ui renderer: text / image / table /
 * progress / countdown / alert / quote / badge / stat / avatar.
 */

private const val DEFAULT_IMAGE_HEIGHT = 220
private const val DEFAULT_IMAGE_ASPECT_RATIO = 1.91f

@Composable
internal fun RenderText(node: TextNode) {
    val style = when (node.style) {
        TextNodeStyle.HEADLINE -> DenebType.subject
        TextNodeStyle.TITLE -> DenebType.cardTitle
        TextNodeStyle.BODY -> MaterialTheme.typography.bodyLarge
        TextNodeStyle.CAPTION -> MaterialTheme.typography.bodySmall
        null -> MaterialTheme.typography.bodyLarge
    }
    val color = when (node.color) {
        "primary" -> MaterialTheme.colorScheme.primary
        "secondary" -> MaterialTheme.colorScheme.secondary
        "error" -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.onSurface
    }
    Text(
        text = node.value.replace("**", ""),
        style = style,
        color = color,
        fontWeight = if (node.bold == true || node.value.startsWith("**")) FontWeight.Bold else null,
        fontStyle = if (node.italic == true) FontStyle.Italic else null,
    )
}

/**
 * Full markdown body inside a deneb-ui tree, rendered through the chat markdown
 * pipeline (same renderer as a regular assistant message). Interactivity and UI
 * callbacks pass through so a nested deneb-ui fence inside the markdown — rare,
 * but possible — keeps working; recursion is naturally bounded by the content.
 */
@Composable
internal fun RenderMarkdown(
    node: MarkdownNode,
    isInteractive: Boolean,
    onCallback: (String, Map<String, String>) -> Unit,
) {
    MarkdownContent(
        content = node.value,
        modifier = Modifier.fillMaxWidth(),
        isInteractive = isInteractive,
        onUiCallback = onCallback,
    )
}

@Composable
internal fun RenderImage(node: ImageNode) {
    val height = (node.height ?: DEFAULT_IMAGE_HEIGHT).dp
    val aspectRatio = (node.aspectRatio ?: DEFAULT_IMAGE_ASPECT_RATIO)
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val width = minOf(maxWidth, height * aspectRatio)
        val modifier = Modifier.height(width / aspectRatio).width(width).clip(RoundedCornerShape(6.dp))
        val previewBitmap = LocalPreviewImages.current[node.url]
        if (previewBitmap != null) {
            Image(
                bitmap = previewBitmap,
                contentDescription = node.alt,
                modifier = modifier,
                contentScale = ContentScale.Crop,
            )
        } else {
            coil3.compose.AsyncImage(
                model = node.url,
                contentDescription = node.alt,
                modifier = modifier,
                contentScale = ContentScale.Crop,
            )
        }
    }
}

@Composable
internal fun RenderTable(node: TableNode) {
    val columnCount = maxOf(
        node.headers.size,
        node.rows.maxOfOrNull { it.size } ?: 0,
    )
    if (columnCount == 0) return
    Column(Modifier.fillMaxWidth().wrapContentHeight()) {
        if (node.headers.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                for (index in 0 until columnCount) {
                    Text(
                        text = node.headers.getOrElse(index) { "" },
                        style = MaterialTheme.typography.titleSmall,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
            HorizontalDivider()
        }
        for (row in node.rows) {
            Row(
                Modifier.fillMaxWidth().padding(vertical = 4.dp),
                verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
            ) {
                for (index in 0 until columnCount) {
                    Text(
                        text = row.getOrElse(index) { "" },
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

@Composable
internal fun RenderProgress(node: ProgressNode) {
    Column(Modifier.fillMaxWidth()) {
        if (node.label != null) {
            Text(
                text = node.label,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(bottom = 4.dp),
            )
        }
        if (node.value != null) {
            LinearProgressIndicator(
                progress = { node.value.coerceIn(0f, 1f) },
                modifier = Modifier.fillMaxWidth(),
                drawStopIndicator = {},
                gapSize = 0.dp,
            )
        } else {
            LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth(),
                gapSize = 0.dp,
            )
        }
    }
}

@Composable
internal fun RenderCountdown(
    node: CountdownNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
) {
    val targetMs = remember { Clock.System.now().toEpochMilliseconds() + node.seconds.toLong() * 1000L }
    var remainingSeconds by remember { mutableStateOf<Long>(node.seconds.toLong()) }
    var expired by remember { mutableStateOf(false) }
    val currentOnCallback by rememberUpdatedState(onCallback)

    LaunchedEffect(targetMs) {
        while (true) {
            val diff = (targetMs - Clock.System.now().toEpochMilliseconds()) / 1000L
            remainingSeconds = diff.coerceAtLeast(0L)
            if (diff <= 0L) {
                if (!expired) {
                    expired = true
                    node.id?.let { formState[it] = "0" }
                    try {
                        when (val action = node.action) {
                            is CallbackAction -> {
                                val data = collectFormData(action, formState)
                                currentOnCallback(action.event, data)
                            }

                            is ToggleAction -> {
                                toggleState[action.targetId] = !(toggleState[action.targetId] ?: true)
                            }

                            is OpenUrlAction -> {}

                            is CopyToClipboardAction -> {}

                            null -> {}
                        }
                    } catch (_: Exception) {}
                }
                break
            }
            node.id?.let { formState[it] = diff.toString() }
            delay(1.seconds)
        }
    }

    Column(Modifier.fillMaxWidth()) {
        if (node.label != null) {
            Text(
                text = node.label,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(bottom = 4.dp),
            )
        }
        val h = remainingSeconds / 3600
        val m = (remainingSeconds % 3600) / 60
        val s = remainingSeconds % 60
        val formatted = if (h > 0) {
            "$h:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}"
        } else {
            "${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}"
        }
        Text(
            text = formatted,
            // Big display number rides the subject rung (22) of the DenebType scale.
            style = DenebType.subject,
            color = if (expired) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
        )
    }
}

@Composable
internal fun RenderAlert(node: AlertNode) {
    // Success/warning use the shared status-container pairs from ui/Theme.kt
    // (the roles M3's scheme lacks); error/info stay on their M3 roles.
    val containerColor = when (node.severity) {
        AlertSeverity.SUCCESS -> denebSuccessContainer()
        AlertSeverity.WARNING -> denebWarningContainer()
        AlertSeverity.ERROR -> MaterialTheme.colorScheme.errorContainer
        AlertSeverity.INFO, null -> MaterialTheme.colorScheme.primaryContainer
    }
    val contentColor = when (node.severity) {
        AlertSeverity.SUCCESS -> denebOnSuccessContainer()
        AlertSeverity.WARNING -> denebOnWarningContainer()
        AlertSeverity.ERROR -> MaterialTheme.colorScheme.onErrorContainer
        AlertSeverity.INFO, null -> MaterialTheme.colorScheme.onPrimaryContainer
    }
    Surface(
        color = containerColor,
        contentColor = contentColor,
        shape = RoundedCornerShape(8.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            AlertIcon(node.severity, contentColor, containerColor)
            Spacer(Modifier.width(12.dp))
            Column {
                if (node.title != null) {
                    Text(
                        text = node.title,
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold,
                    )
                    Spacer(Modifier.height(2.dp))
                }
                Text(
                    text = node.message,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
        }
    }
}

@Composable
private fun AlertIcon(severity: AlertSeverity?, contentColor: Color, containerColor: Color) {
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(20.dp)
            .background(contentColor, androidx.compose.foundation.shape.CircleShape),
    ) {
        when (severity) {
            AlertSeverity.SUCCESS -> Icon(Icons.Default.Check, null, Modifier.size(14.dp), tint = containerColor)
            AlertSeverity.ERROR -> Icon(Icons.Default.Close, null, Modifier.size(14.dp), tint = containerColor)
            AlertSeverity.WARNING -> Text("!", style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.Bold, color = containerColor)
            AlertSeverity.INFO, null -> Text("i", style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.Bold, color = containerColor)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun RenderQuote(node: QuoteNode) {
    Row(
        modifier = Modifier.fillMaxWidth().height(IntrinsicSize.Min),
    ) {
        Box(
            modifier = Modifier
                .width(3.dp)
                .fillMaxHeight()
                .background(MaterialTheme.colorScheme.primary, RoundedCornerShape(1.5.dp)),
        )
        Spacer(Modifier.width(12.dp))
        Column {
            Text(
                text = node.text,
                style = MaterialTheme.typography.bodyLarge,
                fontStyle = FontStyle.Italic,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (node.source != null) {
                Spacer(Modifier.height(2.dp))
                Text(
                    text = "— ${node.source}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
internal fun RenderBadge(node: BadgeNode) {
    val backgroundColor = when (node.color) {
        "primary" -> MaterialTheme.colorScheme.primary
        "secondary" -> MaterialTheme.colorScheme.secondary
        "error" -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.primary
    }
    val contentColor = when (node.color) {
        "primary" -> MaterialTheme.colorScheme.onPrimary
        "secondary" -> MaterialTheme.colorScheme.onSecondary
        "error" -> MaterialTheme.colorScheme.onError
        else -> MaterialTheme.colorScheme.onPrimary
    }
    Surface(
        color = backgroundColor,
        contentColor = contentColor,
        shape = RoundedCornerShape(12.dp),
    ) {
        Text(
            text = node.value,
            style = MaterialTheme.typography.labelSmall,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
        )
    }
}

@Composable
internal fun RenderStat(node: StatNode) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier.widthIn(min = 72.dp),
    ) {
        Text(
            text = node.value,
            // Stat value on the subject rung (22); Bold override keeps the metric
            // reading as a number, not a content title (law 3: weight = function).
            style = DenebType.subject,
            fontWeight = FontWeight.Bold,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = node.label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (node.description != null) {
            Text(
                text = node.description,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
internal fun RenderAvatar(node: AvatarNode) {
    val sizeDp = (node.size ?: 40).coerceIn(24, 80).dp
    if (node.imageUrl != null) {
        Surface(
            shape = androidx.compose.foundation.shape.CircleShape,
            color = MaterialTheme.colorScheme.surfaceContainer,
            modifier = Modifier.size(sizeDp),
        ) {
            coil3.compose.AsyncImage(
                model = node.imageUrl,
                contentDescription = node.name,
                modifier = Modifier.size(sizeDp),
            )
        }
    } else if (node.name != null) {
        val initials = node.name.split(" ")
            .filter { it.isNotEmpty() }
            .take(2)
            .joinToString("") { it.first().uppercase() }
        Surface(
            color = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
            shape = androidx.compose.foundation.shape.CircleShape,
            modifier = Modifier.size(sizeDp),
        ) {
            Box(contentAlignment = Alignment.Center, modifier = Modifier.size(sizeDp)) {
                Text(
                    text = initials,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold,
                )
            }
        }
    } else {
        Surface(
            color = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
            shape = androidx.compose.foundation.shape.CircleShape,
            modifier = Modifier.size(sizeDp),
        ) {
            Box(contentAlignment = Alignment.Center, modifier = Modifier.size(sizeDp)) {
                Icon(
                    imageVector = Icons.Default.Person,
                    contentDescription = null,
                    modifier = Modifier.size(sizeDp * 0.6f),
                )
            }
        }
    }
}
