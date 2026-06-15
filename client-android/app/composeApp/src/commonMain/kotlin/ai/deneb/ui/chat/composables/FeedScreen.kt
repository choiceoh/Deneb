package ai.deneb.ui.chat.composables

import ai.deneb.deneb.DenebEmpty
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.ImmutableList

/**
 * The 업무 (work) home: today's work feed as the main screen rather than a modal
 * behind the chat. Unread items sit on top; tapping one marks it 읽음 (seen,
 * client-side — distinct from the server ack the action buttons do) and expands
 * its full body inline, so the report is read here instead of being mirrored into
 * the chat transcript. Read items collect in a section at the bottom.
 */
@Composable
internal fun FeedScreen(
    items: ImmutableList<WorkFeedItem>,
    seenIds: Set<String>,
    onMarkSeen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
) {
    DenebScreenScaffold(title = "피드", onBack = {}, showBack = false) {
        if (items.isEmpty()) {
            DenebEmpty("오늘 받은 피드가 없습니다")
            return@DenebScreenScaffold
        }
        var expandedId by remember { mutableStateOf<String?>(null) }
        val unread = items.filterNot { seenIds.contains(it.id) }
        val read = items.filter { seenIds.contains(it.id) }

        val open: (String) -> Unit = { id ->
            expandedId = if (expandedId == id) null else id
            onMarkSeen(id)
        }

        LazyColumn(Modifier.fillMaxSize()) {
            items(unread.size) { i ->
                FeedRowWithBody(unread[i], expandedId == unread[i].id, open, onRunAction)
            }
            if (read.isNotEmpty()) {
                item { DenebSectionLabel("읽음") }
                items(read.size) { i ->
                    FeedRowWithBody(read[i], expandedId == read[i].id, open, onRunAction)
                }
            }
        }
    }
}

@Composable
private fun FeedRowWithBody(
    item: WorkFeedItem,
    expanded: Boolean,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
) {
    WorkFeedRow(item = item, onOpen = onOpen, onRunAction = onRunAction)
    if (expanded && item.body.isNotBlank()) {
        // Proactive reports are markdown (tables, headings, lists), so render with
        // the full chat renderer — a plain Text leaked raw "| 항목 | 내용 |" pipes and
        // "##" markers (broken tables). Read-only (isInteractive = false); wrapped in
        // SelectionContainer so the report stays copyable.
        SelectionContainer {
            MarkdownContent(
                content = item.body,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 20.dp, end = 20.dp, top = 4.dp, bottom = 12.dp),
            )
        }
    }
}
