package ai.deneb.deneb

import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Recent-diary timeline (`miniapp.memory.diary_recent`). Deneb writes a daily
 * diary as part of normal operation; this is the "what's been happening lately
 * in my world" view — a vertical list of entries, newest first, each rendered
 * as markdown. Read-only (the agent and the dreamer own diary writes).
 */
@Composable
fun DenebDiaryScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var entries by remember { mutableStateOf<List<DiaryEntry>?>(null) }
    var loadFailed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        loadFailed = false
        entries = null
        val e = client.fetchRecentDiary()
        entries = e
        loadFailed = e == null
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "최근 일기", onBack = onBack, tabBar = navigationTabBar) {
        val list = entries
        when {
            list == null && loadFailed -> StateColumn {
                DenebError("일기를 불러오지 못했습니다.", onRetry = { scope.launch { load() } })
            }

            list == null -> StateColumn { DenebLoading() }

            list.isEmpty() -> StateColumn { DenebEmpty("아직 기록된 일기가 없습니다.") }

            else -> {
                // LazyColumn so only on-screen entries compose — crucially, each
                // entry parses markdown, so an eager Column would parse every body
                // (including off-screen ones) the moment the screen opens.
                LazyColumn(
                    Modifier
                        .fillMaxWidth()
                        .weight(1f)
                        .padding(horizontal = 24.dp),
                    contentPadding = PaddingValues(vertical = 12.dp),
                ) {
                    // Positional keys: diary entries can share a file and may have
                    // blank/duplicate headers, so a content-derived key risks a
                    // duplicate-key crash. The list is fully reloaded (never mutated
                    // in place), so positional identity is correct here.
                    itemsIndexed(list) { index, entry ->
                        Column(Modifier.fillMaxWidth()) {
                            if (entry.header.isNotBlank()) {
                                // Newest-first list: the first entry is the current day —
                                // mark only its title in the cool interactive accent.
                                Text(
                                    entry.header,
                                    style = DenebType.subject,
                                    color = if (index == 0) {
                                        MaterialTheme.colorScheme.primary
                                    } else {
                                        MaterialTheme.colorScheme.onSurface
                                    },
                                )
                                Spacer(Modifier.height(4.dp))
                            }
                            MarkdownContent(entry.content.ifBlank { "(빈 항목)" }, baseStyle = MaterialTheme.typography.bodyMedium)
                            Spacer(Modifier.height(12.dp))
                            HorizontalDivider(color = denebHairline())
                            Spacer(Modifier.height(12.dp))
                        }
                    }
                }
            }
        }
    }
}

/** Weighted, padded container for the loading/error/empty states so they fill
 *  the body (keeping any bottom tab bar pinned) and align under the header,
 *  matching the list's horizontal inset. */
@Composable
private fun ColumnScope.StateColumn(content: @Composable () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .weight(1f)
            .padding(horizontal = 24.dp, vertical = 12.dp),
    ) {
        content()
    }
}
