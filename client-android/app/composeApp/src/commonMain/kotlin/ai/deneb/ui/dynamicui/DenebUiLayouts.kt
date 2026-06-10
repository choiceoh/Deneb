@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.material.icons.filled.Map
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.deneb.ui.denebExpandIn
import ai.deneb.ui.denebShrinkOut
import ai.deneb.ui.handCursor
import ai.deneb.ui.denebAdaptiveCardBorder
import ai.deneb.ui.denebAdaptiveCardColors

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
    @OptIn(ExperimentalLayoutApi::class)
    FlowRow(
        horizontalArrangement = if (allStats) Arrangement.SpaceEvenly else Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .fillMaxWidth()
            .wrapContentHeight(),
    ) {
        for (child in node.children) {
            RenderNode(child, isInteractive, formState, toggleState, onCallback, depth + 1)
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
    Card(
        modifier = Modifier.fillMaxWidth().wrapContentHeight(),
        colors = denebAdaptiveCardColors(),
        border = denebAdaptiveCardBorder(),
    ) {
        Column(
            modifier = Modifier.padding(16.dp).wrapContentHeight(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            RenderChildren(node.children, isInteractive, formState, toggleState, onCallback, depth)
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
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        for ((index, item) in node.items.withIndex()) {
            Row {
                val prefix = if (node.ordered == true) "${index + 1}. " else "\u2022 "
                Text(prefix, style = MaterialTheme.typography.bodyLarge)
                Column(Modifier.weight(1f)) {
                    RenderNode(item, isInteractive, formState, toggleState, onCallback, depth + 1)
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
