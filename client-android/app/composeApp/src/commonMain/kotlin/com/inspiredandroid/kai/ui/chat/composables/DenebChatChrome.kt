@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.handCursor
import kotlinx.collections.immutable.ImmutableList

// Deneb-specific chat chrome: the left navigation drawer (analysis surfaces)
// and the top topic switcher. Kept out of ChatScreen.kt to hold that file
// under the size guideline; the chat UI stays free of any deneb-package import
// by speaking these UI-neutral types ([TopicTab]) and primitive callbacks.

/** One topic tab in the switcher: [key] is sent back on select, [label] shown. */
data class TopicTab(val key: String, val label: String)

/**
 * Horizontal topic switcher rendered just under the top bar. Mirrors the
 * Telegram forum topics (업무 / 잡담 / 코딩); selecting one repoints the chat at
 * that topic's session and per-topic knowledge. Renders nothing when there are
 * fewer than two topics (a single topic needs no switch).
 */
@Composable
fun DenebTopicSwitcher(
    topics: ImmutableList<TopicTab>,
    selectedKey: String?,
    onSelectTopic: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (topics.size < 2) return
    SingleChoiceSegmentedButtonRow(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 4.dp),
    ) {
        topics.forEachIndexed { index, topic ->
            SegmentedButton(
                selected = topic.key == selectedKey,
                onClick = { onSelectTopic(topic.key) },
                shape = SegmentedButtonDefaults.itemShape(index = index, count = topics.size),
                modifier = Modifier.handCursor(),
            ) {
                Text(topic.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * Left navigation drawer content: the analysis surfaces (검색 / 메일 / 일정 /
 * 사람 / 카테고리). Each item invokes its open callback and then [onClose] so the
 * drawer dismisses as navigation pushes the destination.
 */
@Composable
fun DenebDrawerSheet(
    onOpenSearch: () -> Unit,
    onOpenMail: () -> Unit,
    onOpenCalendar: () -> Unit,
    onOpenPeople: () -> Unit,
    onOpenCategories: () -> Unit,
    onClose: () -> Unit,
) {
    ModalDrawerSheet {
        Text(
            text = "Deneb",
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.padding(start = 28.dp, top = 20.dp, bottom = 12.dp),
        )
        HorizontalDivider()
        DrawerItem("검색", Icons.Filled.Search) { onOpenSearch(); onClose() }
        DrawerItem("메일", Icons.Filled.Email) { onOpenMail(); onClose() }
        DrawerItem("일정", Icons.Filled.DateRange) { onOpenCalendar(); onClose() }
        DrawerItem("사람", Icons.Filled.Person) { onOpenPeople(); onClose() }
        DrawerItem("카테고리", Icons.Filled.Folder) { onOpenCategories(); onClose() }
    }
}

@Composable
private fun DrawerItem(
    label: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    onClick: () -> Unit,
) {
    NavigationDrawerItem(
        label = { Text(label) },
        icon = { Icon(icon, contentDescription = null) },
        selected = false,
        onClick = onClick,
        modifier = Modifier.padding(NavigationDrawerItemDefaults.ItemPadding).handCursor(),
    )
}
