package ai.deneb.deneb

import ai.deneb.DenebCategories
import ai.deneb.DenebDiary
import ai.deneb.DenebSearch
import ai.deneb.DenebTodo
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebPressable
import ai.deneb.ui.handCursor
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

private data class MoreEntry(
    val label: String,
    val dest: Any,
    val icon: ImageVector,
    // 업무 데이터 entry: hidden in the 챗봇 workspace (검색·카테고리). 할일·일기 stay.
    val workData: Boolean = false,
)

private val moreEntries = listOf(
    MoreEntry("검색", DenebSearch, Icons.Outlined.Search, workData = true),
    MoreEntry("할일", DenebTodo, Icons.Outlined.CheckCircle),
    MoreEntry("일기", DenebDiary, Icons.AutoMirrored.Outlined.MenuBook),
    MoreEntry("카테고리", DenebCategories, Icons.Outlined.GridView, workData = true),
)

/**
 * The 더보기 screen — the secondary sections that don't fit the four-tab bottom bar,
 * as a full page (not a sheet) so it reads like the other sections and keeps the
 * 더보기 tab in the navigation model (the bottom bar stays visible with 더보기 active).
 * Icon + label rows in the nav-icon idiom; the host wires [onOpen] to navigate.
 */
@Composable
fun DenebMoreScreen(onBack: () -> Unit, onOpen: (Any) -> Unit, chatMode: Boolean = false) {
    val entries = if (chatMode) moreEntries.filterNot { it.workData } else moreEntries
    DenebScreenScaffold(title = "더보기", onBack = onBack) {
        entries.forEach { entry ->
            MoreRow(entry.label, entry.icon) { onOpen(entry.dest) }
        }
    }
}

@Composable
private fun MoreRow(label: String, icon: ImageVector, onClick: () -> Unit) {
    val haptics = rememberHaptics()
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = {
                haptics.tap()
                onClick()
            })
            .handCursor()
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onBackground,
            modifier = Modifier.size(22.dp),
        )
        Spacer(Modifier.width(18.dp))
        Text(label, style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
    }
}
