@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import ai.deneb.ui.denebAdaptiveCardBorder
import ai.deneb.ui.denebAdaptiveCardColors
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.handCursor
import ai.deneb.ui.icons.filled.Map
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.collections.immutable.toImmutableList

/**
 * Layout containers of the deneb-ui renderer: column / row / card / list /
 * box / tabs / accordion. Each delegates child rendering back to RenderNode /
 * RenderChildren in DenebUiRenderer.kt.
 */

@Composable
internal fun RenderColumn(
    node: ColumnNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    Column(
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .fillMaxWidth()
            .wrapContentHeight(),
    ) {
        RenderChildren(node.children, isInteractive, formState, toggleState, onCallback, depth)
    }
}

@Composable
internal fun RenderRow(
    node: RowNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    val allStats = node.children.isNotEmpty() && node.children.all { it is StatNode }
    // longpress="event": press-hold the row to fire the callback (e.g. a
    // morning-card deadline row → mark done). Interactive rows only; the
    // combinedClickable long-press haptic fires automatically (foundation 1.9+).
    val longPress = node.longPressAction as? CallbackAction
    // Immediate local feedback: once long-pressed, strike through + dim the row
    // so "완료" reads instantly, even though the durable state (wiki due_done)
    // and the card body only refresh on the next cycle. Keyed on the action so
    // it persists across recompositions of the same card.
    var marked by remember(longPress) { mutableStateOf(false) }
    val strikeColor = MaterialTheme.colorScheme.onSurface
    var rowModifier: Modifier = Modifier.fillMaxWidth().wrapContentHeight()
    if (isInteractive && longPress != null) {
        @OptIn(ExperimentalFoundationApi::class)
        rowModifier = rowModifier.combinedClickable(
            onClick = {},
            onLongClick = {
                marked = true
                onCallback(longPress.event, longPress.dataAsStrings.orEmpty())
            },
        )
    }
    if (marked) {
        rowModifier = rowModifier
            .alpha(0.5f)
            .drawWithContent {
                drawContent()
                val y = size.height / 2f
                drawLine(strikeColor, Offset(0f, y), Offset(size.width, y), strokeWidth = 2f)
            }
    }
    @OptIn(ExperimentalLayoutApi::class)
    FlowRow(
        // Weighted stat cells own the distribution — SpaceEvenly on top
        // would re-insert edge gaps around the weighted cells (review
        // catch on #3231).
        horizontalArrangement = if (allStats) Arrangement.Start else Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = rowModifier,
    ) {
        for (child in node.children) {
            if (allStats) {
                // Equal-weight grid, not SpaceEvenly: with 2 stats SpaceEvenly
                // parks them at the 1/3 and 2/3 marks (lopsided dead space on
                // the left — visible in the morning-letter FX card). Weighted
                // cells center each stat in its own equal share.
                Box(
                    modifier = Modifier.weight(1f),
                    contentAlignment = Alignment.Center,
                ) {
                    RenderNode(child, isInteractive, formState, toggleState, onCallback, depth + 1)
                }
            } else {
                // Center mixed-height siblings on the row's vertical axis: a
                // headline temperature next to a caption ("18°" + "체감 16°")
                // or body text next to a badge otherwise top-align and look
                // broken (the caption floats at the numeral's cap height).
                Box(modifier = Modifier.align(Alignment.CenterVertically)) {
                    RenderNode(child, isInteractive, formState, toggleState, onCallback, depth + 1)
                }
            }
        }
    }
}

@Composable
internal fun RenderCard(
    node: CardNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    // The letter/briefing convention puts an icon + caption row first — that
    // row IS the card's section label, so give it the section-label voice
    // (tracked SemiBold small caps feel; Korean has no case, letter spacing
    // and weight carry it) instead of rendering as an ordinary caption line.
    val header = (node.children.firstOrNull() as? RowNode)?.takeIf { row ->
        row.children.size == 2 && row.children[0] is IconNode &&
            (row.children[1] as? TextNode)?.style == TextNodeStyle.CAPTION
    }
    Card(
        modifier = Modifier.fillMaxWidth().wrapContentHeight(),
        colors = denebAdaptiveCardColors(),
        border = denebAdaptiveCardBorder(),
    ) {
        Column(
            modifier = Modifier.padding(16.dp).wrapContentHeight(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (header != null) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    val iconNode = header.children[0] as IconNode
                    val glyph = resolveIcon(iconNode.name)
                    if (glyph != null) {
                        Icon(
                            imageVector = glyph,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.size((iconNode.size ?: 16).dp),
                        )
                    } else {
                        // Emoji icon names ("⚠️") have no vector — keep the
                        // generic renderer's fallback instead of dropping the
                        // glyph just because the header path special-cases it
                        // (review catch on #3233).
                        RenderNode(header.children[0], isInteractive, formState, toggleState, onCallback, depth + 1)
                    }
                    Text(
                        text = (header.children[1] as TextNode).value,
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.SemiBold,
                        letterSpacing = 1.1.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                RenderChildren(node.children.drop(1).toImmutableList(), isInteractive, formState, toggleState, onCallback, depth)
            } else {
                RenderChildren(node.children, isInteractive, formState, toggleState, onCallback, depth)
            }
        }
    }
}

@Composable
internal fun RenderList(
    node: ListNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    // Schedule shape: when every item is plain text with an HH:MM key, the
    // list renders as a timeline — right-aligned time column, accent dot,
    // title — instead of generic bullets. The morning letter's 오늘 일정 and
    // any agent briefing get this for free.
    val timeRe = remember { Regex("^\\d{1,2}:\\d{2}\\s*—\\s*(.*)$") }
    val timeline = node.ordered != true && node.items.isNotEmpty() && node.items.all {
        it is TextNode && it.style == null && timeRe.matches(it.value.trim())
    }
    if (timeline) {
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            for (item in node.items) {
                val value = (item as TextNode).value.trim()
                val time = value.substringBefore("—").trim()
                val title = value.substringAfter("—").trim()
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = time,
                        style = MaterialTheme.typography.labelLarge,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                        textAlign = TextAlign.End,
                        modifier = Modifier.width(48.dp),
                    )
                    Box(
                        modifier = Modifier
                            .padding(horizontal = 10.dp)
                            .size(5.dp)
                            .clip(RoundedCornerShape(50))
                            .background(MaterialTheme.colorScheme.primary),
                    )
                    Text(
                        // Inline markdown resolves here too — a raw append
                        // leaked literal ** when models emphasize a title.
                        text = denebUiInlineText(title),
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        for ((index, item) in node.items.withIndex()) {
            Row {
                val prefix = if (node.ordered == true) "${index + 1}. " else "\u2022 "
                Text(prefix, style = MaterialTheme.typography.bodyLarge)
                Column(Modifier.weight(1f)) {
                    // Plain text items take the briefing prefix convention
                    // ("09:00 \u2014 \ud68c\uc758", "\uae40\ubd80\uc7a5 \u2014 \uc81c\ubaa9": SemiBold key before the
                    // em-dash) \u2014 schedules and digests scan by that key.
                    if (item is TextNode && item.style == null && item.bold != true && item.color == null) {
                        Text(
                            text = denebUiListItemText(item.value),
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    } else {
                        RenderNode(item, isInteractive, formState, toggleState, onCallback, depth + 1)
                    }
                }
            }
        }
    }
}

// --- New component renderers ---

@Composable
internal fun RenderBox(
    node: BoxNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    // LLMs frequently misuse box when they mean column, causing children to stack/overlap.
    // Only use Box layout for single-child centering; fall back to Column for multiple children.
    if (node.children.size <= 1 && node.contentAlignment != null) {
        val alignment = when (node.contentAlignment) {
            "center" -> Alignment.Center
            "top_start" -> Alignment.TopStart
            "top_center" -> Alignment.TopCenter
            "top_end" -> Alignment.TopEnd
            "center_start" -> Alignment.CenterStart
            "center_end" -> Alignment.CenterEnd
            "bottom_start" -> Alignment.BottomStart
            "bottom_center" -> Alignment.BottomCenter
            "bottom_end" -> Alignment.BottomEnd
            else -> Alignment.TopStart
        }
        Box(
            contentAlignment = alignment,
            modifier = Modifier.fillMaxWidth().wrapContentHeight(),
        ) {
            for (child in node.children) {
                RenderNode(child, isInteractive, formState, toggleState, onCallback, depth + 1)
            }
        }
    } else {
        Column(
            verticalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.fillMaxWidth().wrapContentHeight(),
        ) {
            RenderChildren(node.children, isInteractive, formState, toggleState, onCallback, depth)
        }
    }
}

@Composable
internal fun RenderTabs(
    node: TabsNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    if (node.tabs.isEmpty()) return
    var selectedIndex by remember { mutableIntStateOf((node.selectedIndex ?: 0).coerceIn(0, node.tabs.lastIndex)) }
    val pillShape = RoundedCornerShape(50)

    Column(Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .layout { measurable, constraints ->
                    val bleed = 12.dp.roundToPx()
                    val wider = if (constraints.maxWidth == Int.MAX_VALUE) {
                        constraints.maxWidth
                    } else {
                        constraints.maxWidth + bleed * 2
                    }
                    val placeable = measurable.measure(
                        constraints.copy(minWidth = 0, maxWidth = wider),
                    )
                    layout(wider, placeable.height) {
                        placeable.place(0, 0)
                    }
                }
                .horizontalScroll(rememberScrollState()),
        ) {
            Spacer(Modifier.width(12.dp))
            Row(
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                modifier = Modifier
                    .clip(pillShape)
                    .background(MaterialTheme.colorScheme.surfaceContainerHigh, pillShape)
                    .padding(4.dp),
            ) {
                node.tabs.forEachIndexed { index, tab ->
                    val isSelected = selectedIndex == index
                    Box(
                        contentAlignment = Alignment.Center,
                        modifier = Modifier
                            .height(32.dp)
                            .clip(pillShape)
                            .then(
                                if (isSelected) {
                                    Modifier.background(
                                        MaterialTheme.colorScheme.primary.copy(alpha = 0.15f),
                                        pillShape,
                                    )
                                } else {
                                    Modifier
                                },
                            )
                            .clickable { selectedIndex = index }
                            .handCursor()
                            .padding(horizontal = 16.dp),
                    ) {
                        Text(
                            text = tab.label,
                            style = MaterialTheme.typography.labelLarge,
                            fontWeight = if (isSelected) FontWeight.SemiBold else FontWeight.Normal,
                            color = if (isSelected) {
                                MaterialTheme.colorScheme.primary
                            } else {
                                MaterialTheme.colorScheme.onSurfaceVariant
                            },
                            maxLines = 1,
                        )
                    }
                }
            }
            Spacer(Modifier.width(12.dp))
        }

        val selectedTab = node.tabs.getOrNull(selectedIndex)
        if (selectedTab != null) {
            Column(
                verticalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
            ) {
                RenderChildren(selectedTab.children, isInteractive, formState, toggleState, onCallback, depth)
            }
        }
    }
}

@Composable
internal fun RenderAccordion(
    node: AccordionNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    var expanded by remember { mutableStateOf(node.expanded ?: false) }

    Surface(
        onClick = { expanded = !expanded },
        modifier = Modifier.fillMaxWidth().handCursor(),
        shape = RoundedCornerShape(6.dp),
        color = MaterialTheme.colorScheme.surfaceContainerLow,
    ) {
        Column(Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = node.title,
                    style = MaterialTheme.typography.titleSmall,
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    imageVector = if (expanded) Icons.Default.KeyboardArrowUp else Icons.Default.KeyboardArrowDown,
                    contentDescription = null,
                )
            }
            AnimatedVisibility(
                visible = expanded,
                enter = denebExpandIn,
                exit = denebShrinkOut,
            ) {
                Column(
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, bottom = 12.dp),
                ) {
                    RenderChildren(node.children, isInteractive, formState, toggleState, onCallback, depth)
                }
            }
        }
    }
}

// --- Icon resolution ---
