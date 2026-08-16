@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import ai.deneb.ui.DenebMotion
import ai.deneb.ui.denebOnSuccessContainer
import ai.deneb.ui.denebOnWarningContainer
import ai.deneb.ui.icons.Visibility
import ai.deneb.ui.icons.VisibilityOff
import ai.deneb.ui.icons.automirrored.filled.Chat
import ai.deneb.ui.icons.automirrored.filled.HelpOutline
import ai.deneb.ui.icons.automirrored.filled.Label
import ai.deneb.ui.icons.automirrored.filled.Redo
import ai.deneb.ui.icons.automirrored.filled.ShowChart
import ai.deneb.ui.icons.automirrored.filled.Sort
import ai.deneb.ui.icons.automirrored.filled.TrendingDown
import ai.deneb.ui.icons.automirrored.filled.TrendingFlat
import ai.deneb.ui.icons.automirrored.filled.TrendingUp
import ai.deneb.ui.icons.automirrored.filled.Undo
import ai.deneb.ui.icons.filled.AccessTime
import ai.deneb.ui.icons.filled.Alarm
import ai.deneb.ui.icons.filled.Analytics
import ai.deneb.ui.icons.filled.AttachFile
import ai.deneb.ui.icons.filled.BarChart
import ai.deneb.ui.icons.filled.BatteryFull
import ai.deneb.ui.icons.filled.Bluetooth
import ai.deneb.ui.icons.filled.Bolt
import ai.deneb.ui.icons.filled.Bookmark
import ai.deneb.ui.icons.filled.BugReport
import ai.deneb.ui.icons.filled.Category
import ai.deneb.ui.icons.filled.Celebration
import ai.deneb.ui.icons.filled.Cloud
import ai.deneb.ui.icons.filled.Code
import ai.deneb.ui.icons.filled.ContentCopy
import ai.deneb.ui.icons.filled.ContentCut
import ai.deneb.ui.icons.filled.ContentPaste
import ai.deneb.ui.icons.filled.DarkMode
import ai.deneb.ui.icons.filled.Dashboard
import ai.deneb.ui.icons.filled.Description
import ai.deneb.ui.icons.filled.DirectionsCar
import ai.deneb.ui.icons.filled.Download
import ai.deneb.ui.icons.filled.Eco
import ai.deneb.ui.icons.filled.EmojiEvents
import ai.deneb.ui.icons.filled.Explore
import ai.deneb.ui.icons.filled.FilterList
import ai.deneb.ui.icons.filled.FitnessCenter
import ai.deneb.ui.icons.filled.Flag
import ai.deneb.ui.icons.filled.Flight
import ai.deneb.ui.icons.filled.Folder
import ai.deneb.ui.icons.filled.Healing
import ai.deneb.ui.icons.filled.History
import ai.deneb.ui.icons.filled.Hotel
import ai.deneb.ui.icons.filled.Image
import ai.deneb.ui.icons.filled.Inventory
import ai.deneb.ui.icons.filled.Language
import ai.deneb.ui.icons.filled.LightMode
import ai.deneb.ui.icons.filled.Lightbulb
import ai.deneb.ui.icons.filled.Link
import ai.deneb.ui.icons.filled.LocalCafe
import ai.deneb.ui.icons.filled.LockOpen
import ai.deneb.ui.icons.filled.Map
import ai.deneb.ui.icons.filled.MilitaryTech
import ai.deneb.ui.icons.filled.Movie
import ai.deneb.ui.icons.filled.Pause
import ai.deneb.ui.icons.filled.Payments
import ai.deneb.ui.icons.filled.Pets
import ai.deneb.ui.icons.filled.PieChart
import ai.deneb.ui.icons.filled.Public
import ai.deneb.ui.icons.filled.PushPin
import ai.deneb.ui.icons.filled.Receipt
import ai.deneb.ui.icons.filled.Restaurant
import ai.deneb.ui.icons.filled.RocketLaunch
import ai.deneb.ui.icons.filled.Savings
import ai.deneb.ui.icons.filled.School
import ai.deneb.ui.icons.filled.Science
import ai.deneb.ui.icons.filled.Security
import ai.deneb.ui.icons.filled.SkipNext
import ai.deneb.ui.icons.filled.SkipPrevious
import ai.deneb.ui.icons.filled.Speed
import ai.deneb.ui.icons.filled.Stop
import ai.deneb.ui.icons.filled.SwapHoriz
import ai.deneb.ui.icons.filled.Sync
import ai.deneb.ui.icons.filled.TaskAlt
import ai.deneb.ui.icons.filled.Terminal
import ai.deneb.ui.icons.filled.ThumbDown
import ai.deneb.ui.icons.filled.Timer
import ai.deneb.ui.icons.filled.Translate
import ai.deneb.ui.icons.filled.Upload
import ai.deneb.ui.icons.filled.Verified
import ai.deneb.ui.icons.filled.WaterDrop
import ai.deneb.ui.icons.filled.WbSunny
import ai.deneb.ui.icons.filled.Wifi
import ai.deneb.ui.icons.filled.Work
import ai.deneb.ui.icons.filled.WorkspacePremium
import ai.deneb.ui.markdown.CodeFenceBlock
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.Call
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Face
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.ShoppingCart
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.ThumbUp
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * Drawn / monospace components of the deneb-ui renderer: canvas charts, code
 * blocks, and the named-icon catalog (resolveIcon).
 */

/**
 * Single-series bar or line chart drawn on a Canvas. Display-only (no
 * interaction). Values are normalized to the series max. Everything —
 * value labels, x-axis labels, baseline, gridline — is drawn inside the
 * canvas with a TextMeasurer so labels stay center-aligned with their bars
 * and points (a Row of SpaceBetween texts drifts off bar centers).
 */
@Composable
internal fun RenderChart(node: ChartNode) {
    val values = node.values
    if (values.isEmpty()) return
    val maxValue = values.maxOrNull()?.takeIf { it > 0f } ?: 1f
    val chartColor = MaterialTheme.colorScheme.primary
    val valueColor = MaterialTheme.colorScheme.onSurface
    val axisColor = MaterialTheme.colorScheme.onSurfaceVariant
    val gridColor = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f)
    val textMeasurer = rememberTextMeasurer()
    val valueStyle = MaterialTheme.typography.labelSmall
    val axisStyle = MaterialTheme.typography.labelSmall

    // Value labels above bars/points, drawn as compact numbers: an integer
    // series stays integer ("381"), fractional values keep one decimal.
    fun fmt(v: Float): String = if (v == kotlin.math.floor(v)) v.toInt().toString() else ((kotlin.math.round(v * 10f)) / 10f).toString()

    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        node.label?.takeIf { it.isNotBlank() }?.let {
            Text(
                text = it,
                style = MaterialTheme.typography.titleSmall,
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        // Entrance: the chart draws itself in (line trims along its length,
        // bars grow). Static contexts (preview harness) pin progress at 1.
        val motion = LocalDenebUiMotion.current
        val drawAnim = remember { Animatable(if (motion) 0f else 1f) }
        LaunchedEffect(motion) {
            // React to the switch in BOTH directions: a flip to static must
            // snap to fully drawn, not strand a partial chart (review catch
            // on #3234).
            if (motion) {
                drawAnim.animateTo(
                    1f,
                    animationSpec = tween(durationMillis = DenebMotion.DurationChartDraw, easing = FastOutSlowInEasing),
                )
            } else {
                drawAnim.snapTo(1f)
            }
        }
        val drawProgress = drawAnim.value

        // Labels are drawn inside the canvas, so surface the series to
        // accessibility explicitly (review catch on #3228).
        val chartSummary = values.mapIndexed { i, v ->
            "${node.labels.getOrNull(i) ?: (i + 1)} ${fmt(v)}"
        }.joinToString(", ")
        Canvas(
            modifier = Modifier
                .fillMaxWidth()
                .height(148.dp)
                .semantics { contentDescription = "${node.label ?: "차트"}: $chartSummary" },
        ) {
            val w = size.width
            val valueBand = 16.dp.toPx() // headroom for value labels
            val axisBand = if (node.labels.isNotEmpty()) 18.dp.toPx() else 2.dp.toPx()
            val plotTop = valueBand
            val plotBottom = size.height - axisBand
            val plotH = (plotBottom - plotTop).coerceAtLeast(1f)

            fun drawCenteredText(text: String, centerX: Float, top: Float, style: androidx.compose.ui.text.TextStyle, color: androidx.compose.ui.graphics.Color) {
                val layout = textMeasurer.measure(text, style)
                val x = (centerX - layout.size.width / 2f).coerceIn(0f, (w - layout.size.width).coerceAtLeast(0f))
                drawText(layout, color = color, topLeft = Offset(x, top))
            }

            // Baseline + one mid gridline: enough scaffolding to read scale
            // without turning the card into a spreadsheet (2-accent restraint).
            drawLine(gridColor, Offset(0f, plotBottom), Offset(w, plotBottom), strokeWidth = 1.dp.toPx())
            drawLine(gridColor, Offset(0f, plotTop + plotH / 2f), Offset(w, plotTop + plotH / 2f), strokeWidth = 1.dp.toPx())

            if (node.chartType == "line") {
                val stepX = if (values.size >= 2) w / (values.size - 1) else 0f
                fun xAt(i: Int) = if (values.size >= 2) i * stepX else w / 2f
                // Line charts read trend, not magnitude: scale from a padded
                // min so a 275→381 series uses the full plot instead of
                // hugging the top third over a dead zero-zone. Honest because
                // every point carries its real value label; BAR charts stay
                // zero-based (their shape IS the magnitude claim).
                val minValue = values.min()
                val span = (maxValue - minValue).takeIf { it > 0f } ?: maxValue
                // Floor the padded baseline at 0 for all-positive series (no
                // phantom negative axis under real data), but let it go below
                // zero when the series actually dips negative so those points
                // stay inside the plot instead of drawing under the baseline.
                val paddedLo = minValue - span * 0.15f
                val lo = if (minValue < 0f) paddedLo else paddedLo.coerceAtLeast(0f)
                val hi = maxValue + span * 0.05f
                fun yAt(v: Float) = plotBottom - ((v - lo) / (hi - lo)) * plotH
                if (values.size >= 2) {
                    // Smooth curve through the points (midpoint control
                    // handles — stays inside the data envelope), with a soft
                    // area wash underneath so the trend reads as a shape,
                    // and a draw-in trim on entrance.
                    val path = Path()
                    path.moveTo(xAt(0), yAt(values[0]))
                    for (i in 1 until values.size) {
                        val x0 = xAt(i - 1)
                        val y0 = yAt(values[i - 1])
                        val x1 = xAt(i)
                        val y1 = yAt(values[i])
                        val mx = (x0 + x1) / 2f
                        path.cubicTo(mx, y0, mx, y1, x1, y1)
                    }
                    val area = Path().apply {
                        addPath(path)
                        lineTo(xAt(values.size - 1), plotBottom)
                        lineTo(xAt(0), plotBottom)
                        close()
                    }
                    drawPath(
                        area,
                        brush = Brush.verticalGradient(
                            colors = listOf(chartColor.copy(alpha = 0.16f), chartColor.copy(alpha = 0f)),
                            startY = plotTop,
                            endY = plotBottom,
                        ),
                    )
                    if (drawProgress < 1f) {
                        val measure = PathMeasure()
                        measure.setPath(path, false)
                        val partial = Path()
                        measure.getSegment(0f, measure.length * drawProgress, partial, true)
                        drawPath(partial, color = chartColor, style = Stroke(width = 2.5.dp.toPx()))
                    } else {
                        drawPath(path, color = chartColor, style = Stroke(width = 2.5.dp.toPx()))
                    }
                }
                values.forEachIndexed { i, v ->
                    val last = i == values.lastIndex
                    if (last) {
                        // The newest point is the reader's answer — halo it.
                        drawCircle(chartColor.copy(alpha = 0.22f), radius = 8.dp.toPx(), center = Offset(xAt(i), yAt(v)))
                        drawCircle(chartColor, radius = 4.5.dp.toPx(), center = Offset(xAt(i), yAt(v)))
                    } else {
                        drawCircle(chartColor, radius = 3.dp.toPx(), center = Offset(xAt(i), yAt(v)))
                    }
                    drawCenteredText(fmt(v), xAt(i), yAt(v) - valueBand, valueStyle, valueColor)
                    node.labels.getOrNull(i)?.let { lbl ->
                        drawCenteredText(lbl, xAt(i), plotBottom + 4.dp.toPx(), axisStyle, axisColor)
                    }
                }
            } else {
                val count = values.size
                val gap = (w * 0.04f).coerceAtLeast(4.dp.toPx())
                val barWidth = ((w - gap * (count + 1)) / count).coerceAtLeast(1f)
                val corner = 3.dp.toPx()
                values.forEachIndexed { index, v ->
                    // Zero is a real claim — draw no bar (the value label
                    // still says 0); only positive values get the legibility
                    // minimum (review catch on #3228).
                    val barHeight = if (v > 0f) (((v / maxValue) * plotH) * drawProgress).coerceAtLeast(2.dp.toPx()) else 0f
                    val x = gap + index * (barWidth + gap)
                    val centerX = x + barWidth / 2f
                    // Vertical gradient (opaque top → faint base) gives the columns
                    // depth instead of a flat fill — matches the line chart's area
                    // wash and the desktop bar gradient.
                    drawRoundRect(
                        brush = Brush.verticalGradient(
                            colors = listOf(chartColor.copy(alpha = 0.95f), chartColor.copy(alpha = 0.30f)),
                            startY = plotBottom - barHeight,
                            endY = plotBottom,
                        ),
                        topLeft = Offset(x, plotBottom - barHeight),
                        size = Size(barWidth, barHeight),
                        cornerRadius = CornerRadius(corner, corner),
                    )
                    drawCenteredText(fmt(v), centerX, plotBottom - barHeight - valueBand, valueStyle, valueColor)
                    node.labels.getOrNull(index)?.let { lbl ->
                        drawCenteredText(lbl, centerX, plotBottom + 4.dp.toPx(), axisStyle, axisColor)
                    }
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

            // Status tints, mirroring RenderText: a check icon next to a
            // success line should carry the same voice as the text.
            "success" -> denebOnSuccessContainer()

            "warning" -> denebOnWarningContainer()

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
    // Same chrome as a chat-prose code fence (header strip with language +
    // copy-with-check, hairline, syntax highlighting) — the old bespoke block
    // floated the copy icon over the code and had no highlighting, so a card's
    // code read visibly poorer than the identical fence in prose.
    CodeFenceBlock(language = node.language, code = node.code)
}

internal fun resolveIcon(name: String): ImageVector? = when (name) {
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
    "swap", "swap_horiz", "compare", "vs" -> Icons.Filled.SwapHoriz
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
    "video", "movie", "film" -> Icons.Filled.Movie
    "message", "chat", "comment" -> Icons.AutoMirrored.Filled.Chat
    "document", "doc", "file", "description" -> Icons.Filled.Description
    "folder" -> Icons.Filled.Folder
    "history" -> Icons.Filled.History
    "help", "question" -> Icons.AutoMirrored.Filled.HelpOutline
    else -> null
}

// --- Form state initialization ---
