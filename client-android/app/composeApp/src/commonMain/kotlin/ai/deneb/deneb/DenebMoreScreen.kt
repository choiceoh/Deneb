package ai.deneb.deneb

import ai.deneb.DenebCategories
import ai.deneb.DenebDiary
import ai.deneb.DenebSearch
import ai.deneb.DenebTodo
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebScreenScaffold
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.Search
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

private data class MoreEntry(
    val label: String,
    val dest: Any,
    val icon: ImageVector,
    val desc: String,
    // 업무 데이터 entry: hidden in the 챗봇 workspace (검색·카테고리). 할일·일기 stay.
    val workData: Boolean = false,
)

private val moreEntries = listOf(
    MoreEntry("검색", DenebSearch, Icons.Outlined.Search, "메일·위키 통합 검색", workData = true),
    MoreEntry("할일", DenebTodo, Icons.Outlined.CheckCircle, "할 일 목록"),
    MoreEntry("일기", DenebDiary, Icons.AutoMirrored.Outlined.MenuBook, "일기 기록"),
    MoreEntry("카테고리", DenebCategories, Icons.Outlined.GridView, "위키 분류, 사람", workData = true),
)

/**
 * The 더보기 screen — the secondary sections that don't fit the four-tab bottom bar,
 * as a full page (not a sheet) so it reads like the other sections and keeps the
 * 더보기 tab in the navigation model (the bottom bar stays visible with 더보기 active).
 *
 * Uses the same grouped inset-card idiom as the settings hub (DenebGroup +
 * DenebListRow: leading icon, title, one-line summary, chevron) so 더보기 and 설정
 * read identically.
 */
@Composable
fun DenebMoreScreen(onBack: () -> Unit, onOpen: (Any) -> Unit, chatMode: Boolean = false) {
    val entries = if (chatMode) moreEntries.filterNot { it.workData } else moreEntries
    DenebScreenScaffold(title = "더보기", onBack = onBack) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(top = 4.dp, bottom = 24.dp),
        ) {
            DenebGroup {
                entries.forEachIndexed { i, entry ->
                    DenebListRow(
                        title = entry.label,
                        onClick = { onOpen(entry.dest) },
                        icon = entry.icon,
                        subtitle = entry.desc,
                        divider = i < entries.lastIndex,
                    )
                }
            }
        }
    }
}
