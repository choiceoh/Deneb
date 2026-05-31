@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.handCursor
import kotlinx.collections.immutable.ImmutableList

// Deneb-specific chat chrome: the left navigation drawer (analysis surfaces +
// records/settings) and the top topic switcher. Kept out of ChatScreen.kt to
// hold that file under the size guideline; the chat UI stays free of any
// deneb-package import by speaking these UI-neutral types ([TopicTab]) and
// primitive callbacks.

/** One topic tab in the switcher: [key] is sent back on select, [label] shown. */
data class TopicTab(val key: String, val label: String)

/**
 * Topic switcher rendered just under the top bar — a compact, centered pill
 * group (iOS-style segmented control) mirroring the Telegram forum topics
 * (업무 / 잡담 / 코딩). Selecting one repoints the chat at that topic's session
 * and per-topic knowledge. The selected pill fills with the accent color and
 * animates on switch. Renders nothing with fewer than two topics.
 */
@Composable
fun DenebTopicSwitcher(
    topics: ImmutableList<TopicTab>,
    selectedKey: String?,
    onSelectTopic: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (topics.size < 2) return
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.Center,
    ) {
        Row(
            modifier = Modifier
                .clip(RoundedCornerShape(percent = 50))
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .padding(3.dp),
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            topics.forEach { topic ->
                val selected = topic.key == selectedKey
                val pillColor by animateColorAsState(
                    targetValue = if (selected) MaterialTheme.colorScheme.primary else Color.Transparent,
                    label = "topicPillBg",
                )
                val labelColor by animateColorAsState(
                    targetValue = if (selected) {
                        MaterialTheme.colorScheme.onPrimary
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    label = "topicPillFg",
                )
                Box(
                    modifier = Modifier
                        .clip(RoundedCornerShape(percent = 50))
                        .background(pillColor)
                        .clickable { onSelectTopic(topic.key) }
                        .handCursor()
                        .padding(horizontal = 18.dp, vertical = 7.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = topic.label,
                        style = MaterialTheme.typography.labelLarge,
                        color = labelColor,
                        fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

/**
 * Left navigation drawer content. Two groups: the analysis surfaces (검색 / 메일
 * / 일정 / 사람 / 카테고리) and, below a divider, 기록·설정 (대화 기록 + 설정) so
 * those live one swipe away instead of crowding the top bar. Each item invokes
 * its callback and then [onClose] so the drawer dismisses as navigation pushes.
 */
@Composable
fun DenebDrawerSheet(
    onOpenSearch: () -> Unit,
    onOpenMail: () -> Unit,
    onOpenCalendar: () -> Unit,
    onOpenPeople: () -> Unit,
    onOpenCategories: () -> Unit,
    onShowHistory: () -> Unit,
    onNavigateToSettings: () -> Unit,
    hasSavedConversations: Boolean,
    onClose: () -> Unit,
) {
    ModalDrawerSheet {
        Text(
            text = "Deneb",
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.padding(start = 28.dp, top = 20.dp, bottom = 12.dp),
        )
        HorizontalDivider()
        DrawerSectionLabel("분석")
        DrawerItem("검색", Icons.Filled.Search) { onOpenSearch(); onClose() }
        DrawerItem("메일", Icons.Filled.Email) { onOpenMail(); onClose() }
        DrawerItem("일정", Icons.Filled.DateRange) { onOpenCalendar(); onClose() }
        DrawerItem("사람", Icons.Filled.Person) { onOpenPeople(); onClose() }
        DrawerItem("카테고리", Icons.Filled.Folder) { onOpenCategories(); onClose() }

        Spacer(Modifier.height(8.dp))
        HorizontalDivider()
        DrawerSectionLabel("기록·설정")
        if (hasSavedConversations) {
            DrawerItem("대화 기록", Icons.Filled.History) { onShowHistory(); onClose() }
        }
        DrawerItem("설정", Icons.Filled.Settings) { onNavigateToSettings(); onClose() }
    }
}

@Composable
private fun DrawerSectionLabel(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 28.dp, top = 16.dp, bottom = 4.dp),
    )
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
