@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.automirrored.filled.Label
import androidx.compose.material.icons.automirrored.filled.Redo
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.automirrored.filled.ShowChart
import androidx.compose.material.icons.automirrored.filled.Sort
import androidx.compose.material.icons.automirrored.filled.TrendingDown
import androidx.compose.material.icons.automirrored.filled.TrendingFlat
import androidx.compose.material.icons.automirrored.filled.TrendingUp
import androidx.compose.material.icons.automirrored.filled.Undo
import androidx.compose.material.icons.filled.AccessTime
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Alarm
import androidx.compose.material.icons.filled.Analytics
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.BarChart
import androidx.compose.material.icons.filled.BatteryFull
import androidx.compose.material.icons.filled.Bluetooth
import androidx.compose.material.icons.filled.Bolt
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.BugReport
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.Call
import androidx.compose.material.icons.filled.Category
import androidx.compose.material.icons.filled.Celebration
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Cloud
import androidx.compose.material.icons.filled.Code
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.ContentCut
import androidx.compose.material.icons.filled.ContentPaste
import androidx.compose.material.icons.filled.DarkMode
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Eco
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.EmojiEvents
import androidx.compose.material.icons.filled.Explore
import androidx.compose.material.icons.filled.Face
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FilterList
import androidx.compose.material.icons.filled.FitnessCenter
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.Flight
import androidx.compose.material.icons.filled.Healing
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Hotel
import androidx.compose.material.icons.filled.Image
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Inventory
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.LightMode
import androidx.compose.material.icons.filled.Lightbulb
import androidx.compose.material.icons.filled.Link
import androidx.compose.material.icons.filled.LocalCafe
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.LockOpen
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.MilitaryTech
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.Payments
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Pets
import androidx.compose.material.icons.filled.PieChart
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Public
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.filled.Receipt
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Restaurant
import androidx.compose.material.icons.filled.RocketLaunch
import androidx.compose.material.icons.filled.Savings
import androidx.compose.material.icons.filled.School
import androidx.compose.material.icons.filled.Science
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.ShoppingCart
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material.icons.filled.Sync
import androidx.compose.material.icons.filled.TaskAlt
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material.icons.filled.ThumbDown
import androidx.compose.material.icons.filled.ThumbUp
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material.icons.filled.Translate
import androidx.compose.material.icons.filled.Upload
import androidx.compose.material.icons.filled.Verified
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material.icons.filled.WaterDrop
import androidx.compose.material.icons.filled.WbSunny
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material.icons.filled.Work
import androidx.compose.material.icons.filled.WorkspacePremium
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.deneb.ui.handCursor
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.deneb_ui_code_copy
import org.jetbrains.compose.resources.stringResource

/**
 * Drawn / monospace components of the deneb-ui renderer: canvas charts, code
 * blocks, and the named-icon catalog (resolveIcon).
 */

/**
 * Single-series bar or line chart drawn on a Canvas. Display-only (no interaction). Values are
 * normalized to the series max; an empty series renders nothing.
 */
@Composable
internal fun RenderChart(node: ChartNode) {
    val values = node.values
    if (values.isEmpty()) return
    val maxValue = values.maxOrNull()?.takeIf { it > 0f } ?: 1f
    val chartColor = MaterialTheme.colorScheme.primary
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        node.label?.takeIf { it.isNotBlank() }?.let {
            Text(
                text = it,
                style = MaterialTheme.typography.titleSmall,
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        Canvas(modifier = Modifier.fillMaxWidth().height(160.dp)) {
            val w = size.width
            val h = size.height
            if (node.chartType == "line") {
                if (values.size >= 2) {
                    val stepX = w / (values.size - 1)
                    val path = Path()
                    values.forEachIndexed { index, v ->
                        val x = index * stepX
                        val y = h - (v / maxValue) * h
                        if (index == 0) path.moveTo(x, y) else path.lineTo(x, y)
                    }
                    drawPath(path, color = chartColor, style = Stroke(width = 3.dp.toPx()))
                } else {
                    val y = h - (values.first() / maxValue) * h
                    drawCircle(chartColor, radius = 4.dp.toPx(), center = Offset(w / 2f, y))
                }
            } else {
                val count = values.size
                val gap = w * 0.02f
                val barWidth = ((w - gap * (count + 1)) / count).coerceAtLeast(1f)
                values.forEachIndexed { index, v ->
                    val barHeight = (v / maxValue) * h
                    val x = gap + index * (barWidth + gap)
                    drawRect(
                        color = chartColor,
                        topLeft = Offset(x, h - barHeight),
                        size = Size(barWidth, barHeight),
                    )
                }
            }
        }
        if (node.labels.isNotEmpty()) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                node.labels.forEach { lbl ->
                    Text(
                        text = lbl,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
internal fun RenderIcon(node: IconNode) {
    val imageVector = resolveIcon(node.name)
    val size = (node.size ?: 24).dp
    if (imageVector != null) {
        val color = when (node.color) {
            "primary" -> MaterialTheme.colorScheme.primary
            "secondary" -> MaterialTheme.colorScheme.secondary
            "error" -> MaterialTheme.colorScheme.error
            else -> MaterialTheme.colorScheme.onSurface
        }
        Icon(
            imageVector = imageVector,
            contentDescription = node.name,
            modifier = Modifier.size(size),
            tint = color,
        )
    } else if (node.name.isNotEmpty() && node.name.any { it.code > 0x2600 }) {
        Text(
            text = node.name,
            fontSize = size.value.sp,
        )
    }
}

@Composable
internal fun RenderCode(node: CodeNode) {
    val clipboardManager = LocalClipboardManager.current
    Surface(
        color = MaterialTheme.colorScheme.surfaceVariant,
        shape = RoundedCornerShape(8.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Box(Modifier.padding(12.dp)) {
            Column {
                if (node.language != null) {
                    Text(
                        text = node.language,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(bottom = 4.dp, end = 32.dp),
                    )
                }
                Text(
                    text = node.code,
                    style = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.horizontalScroll(rememberScrollState()).padding(end = 32.dp),
                )
            }
            Box(
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .size(28.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .handCursor()
                    .clickable { clipboardManager.setText(AnnotatedString(node.code)) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.Filled.ContentCopy,
                    contentDescription = stringResource(Res.string.deneb_ui_code_copy),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
    }
}

private fun resolveIcon(name: String): ImageVector? = when (name) {
    "home" -> Icons.Default.Home
    "settings" -> Icons.Default.Settings
    "search" -> Icons.Default.Search
    "add" -> Icons.Default.Add
    "delete" -> Icons.Default.Delete
    "edit" -> Icons.Default.Edit
    "check", "done" -> Icons.Default.Check
    "check_circle" -> Icons.Default.CheckCircle
    "close" -> Icons.Default.Close
    "arrow_back" -> Icons.AutoMirrored.Filled.ArrowBack
    "arrow_forward" -> Icons.AutoMirrored.Filled.ArrowForward
    "star" -> Icons.Default.Star
    "favorite" -> Icons.Default.Favorite
    "share" -> Icons.Default.Share
    "info" -> Icons.Default.Info
    "warning" -> Icons.Default.Warning
    "person" -> Icons.Default.Person
    "group" -> Icons.Default.Face
    "mail", "email" -> Icons.Default.Email
    "phone" -> Icons.Default.Call
    "calendar", "date_range", "schedule" -> Icons.Default.DateRange
    "clock", "access_time" -> Icons.Filled.AccessTime
    "location", "place" -> Icons.Default.LocationOn
    "photo", "image" -> Icons.Filled.Image
    "refresh" -> Icons.Default.Refresh
    "menu" -> Icons.Default.Menu
    "more", "more_vert" -> Icons.Default.MoreVert
    "send" -> Icons.AutoMirrored.Filled.Send
    "notifications" -> Icons.Default.Notifications
    "expand_more" -> Icons.Default.KeyboardArrowDown
    "expand_less" -> Icons.Default.KeyboardArrowUp
    "trending_up" -> Icons.AutoMirrored.Filled.TrendingUp
    "trending_down" -> Icons.AutoMirrored.Filled.TrendingDown
    "trending_flat" -> Icons.AutoMirrored.Filled.TrendingFlat
    "thumb_up" -> Icons.Default.ThumbUp
    "thumb_down" -> Icons.Filled.ThumbDown
    "visibility" -> Icons.Filled.Visibility
    "visibility_off" -> Icons.Filled.VisibilityOff
    "lock" -> Icons.Default.Lock
    "lock_open" -> Icons.Filled.LockOpen
    "shopping_cart", "cart" -> Icons.Default.ShoppingCart
    "play_arrow", "play" -> Icons.Default.PlayArrow
    "pause" -> Icons.Filled.Pause
    "stop" -> Icons.Filled.Stop
    "skip_next" -> Icons.Filled.SkipNext
    "skip_previous" -> Icons.Filled.SkipPrevious
    "download" -> Icons.Filled.Download
    "upload" -> Icons.Filled.Upload
    "cloud" -> Icons.Filled.Cloud
    "attach_file", "attachment" -> Icons.Filled.AttachFile
    "link" -> Icons.Filled.Link
    "code" -> Icons.Filled.Code
    "terminal" -> Icons.Filled.Terminal
    "build", "construction" -> Icons.Default.Build
    "bug_report", "bug" -> Icons.Filled.BugReport
    "lightbulb", "idea" -> Icons.Filled.Lightbulb
    "science", "flask" -> Icons.Filled.Science
    "school", "education" -> Icons.Filled.School
    "work", "business" -> Icons.Filled.Work
    "account_circle" -> Icons.Default.AccountCircle
    "language", "globe" -> Icons.Filled.Language
    "translate" -> Icons.Filled.Translate
    "dark_mode", "moon" -> Icons.Filled.DarkMode
    "light_mode", "sun" -> Icons.Filled.LightMode
    "bolt", "flash", "lightning" -> Icons.Filled.Bolt
    "rocket_launch", "rocket" -> Icons.Filled.RocketLaunch
    "savings", "money" -> Icons.Filled.Savings
    "payments", "credit_card" -> Icons.Filled.Payments
    "receipt" -> Icons.Filled.Receipt
    "inventory" -> Icons.Filled.Inventory
    "category" -> Icons.Filled.Category
    "dashboard" -> Icons.Filled.Dashboard
    "analytics" -> Icons.Filled.Analytics
    "bar_chart", "chart" -> Icons.Filled.BarChart
    "pie_chart" -> Icons.Filled.PieChart
    "show_chart" -> Icons.AutoMirrored.Filled.ShowChart
    "timer" -> Icons.Filled.Timer
    "alarm" -> Icons.Filled.Alarm
    "task", "task_alt" -> Icons.Filled.TaskAlt
    "bookmark" -> Icons.Filled.Bookmark
    "flag" -> Icons.Filled.Flag
    "label", "tag" -> Icons.AutoMirrored.Filled.Label
    "pin", "push_pin" -> Icons.Filled.PushPin
    "copy", "content_copy" -> Icons.Filled.ContentCopy
    "paste", "content_paste" -> Icons.Filled.ContentPaste
    "cut", "content_cut" -> Icons.Filled.ContentCut
    "undo" -> Icons.AutoMirrored.Filled.Undo
    "redo" -> Icons.AutoMirrored.Filled.Redo
    "filter", "filter_list" -> Icons.Filled.FilterList
    "sort" -> Icons.AutoMirrored.Filled.Sort
    "swap", "swap_horiz" -> Icons.Filled.SwapHoriz
    "sync" -> Icons.Filled.Sync
    "wifi" -> Icons.Filled.Wifi
    "bluetooth" -> Icons.Filled.Bluetooth
    "battery_full", "battery" -> Icons.Filled.BatteryFull
    "speed" -> Icons.Filled.Speed
    "security", "shield" -> Icons.Filled.Security
    "verified" -> Icons.Filled.Verified
    "health", "medical", "healing" -> Icons.Filled.Healing
    "fitness", "fitness_center" -> Icons.Filled.FitnessCenter
    "restaurant", "food" -> Icons.Filled.Restaurant
    "local_cafe", "coffee" -> Icons.Filled.LocalCafe
    "flight", "airplane" -> Icons.Filled.Flight
    "hotel" -> Icons.Filled.Hotel
    "directions_car", "car" -> Icons.Filled.DirectionsCar
    "public", "earth" -> Icons.Filled.Public
    "map" -> Icons.Filled.Map
    "explore", "compass" -> Icons.Filled.Explore
    "pets", "pet" -> Icons.Filled.Pets
    "eco", "leaf", "nature" -> Icons.Filled.Eco
    "water_drop", "water" -> Icons.Filled.WaterDrop
    "sunny", "weather" -> Icons.Filled.WbSunny
    "celebration", "party" -> Icons.Filled.Celebration
    "emoji_events", "trophy" -> Icons.Filled.EmojiEvents
    "military_tech", "medal" -> Icons.Filled.MilitaryTech
    "workspace_premium", "premium" -> Icons.Filled.WorkspacePremium
    else -> null
}

// --- Form state initialization ---
