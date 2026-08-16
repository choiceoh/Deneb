package ai.deneb.deneb

import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.icons.filled.Bookmark
import ai.deneb.ui.icons.outlined.History
import ai.deneb.ui.icons.outlined.Public
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

private const val START_LIST_LIMIT = 8

/**
 * Designed empty / first-open surface: bookmarks and recent visits, or a hint
 * to type in the address bar. Previewable — no WebView, no settings.
 */
@Composable
internal fun BrowserStartPane(
    bookmarks: List<BrowserBookmark>,
    visits: List<BrowserVisit>,
    onOpen: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val startBookmarks = bookmarks.take(START_LIST_LIMIT)
    val startVisits = visits.take(START_LIST_LIMIT)
    Column(
        modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(top = 12.dp, bottom = 24.dp),
    ) {
        if (startBookmarks.isEmpty() && startVisits.isEmpty()) {
            DenebEmpty(
                text = "페이지가 없습니다",
                hint = "아래 주소창에 주소를 입력하거나 검색해 보세요",
                icon = Icons.Outlined.Public,
            )
            return@Column
        }
        if (startBookmarks.isNotEmpty()) {
            DenebGroup(label = "북마크") {
                startBookmarks.forEachIndexed { i, bookmark ->
                    DenebListRow(
                        title = browserBookmarkDisplayTitle(bookmark),
                        subtitle = bookmark.url,
                        icon = Icons.Filled.Bookmark,
                        onClick = { onOpen(bookmark.url) },
                        divider = i < startBookmarks.lastIndex,
                    )
                }
            }
            Spacer(Modifier.height(16.dp))
        }
        if (startVisits.isNotEmpty()) {
            DenebGroup(label = "최근 방문") {
                startVisits.forEachIndexed { i, visit ->
                    DenebListRow(
                        title = browserVisitDisplayTitle(visit),
                        subtitle = visit.url,
                        icon = Icons.Outlined.History,
                        onClick = { onOpen(visit.url) },
                        divider = i < startVisits.lastIndex,
                    )
                }
            }
        }
    }
}
