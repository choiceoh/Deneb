package ai.deneb.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.deneb.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Settings hub "토픽문서" tab: per-topic injected docs with a "+ 새 문서" entry;
 * tapping a row deep-links into the doc editor screen. Hosted by
 * [DenebConfigScreen]'s pager.
 */
@Composable
internal fun TopicDocsTab(client: DenebGatewayClient, onOpenTopicDoc: (String) -> Unit) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    var docs by remember { mutableStateOf<List<TopicDocFile>?>(null) }
    var loadFailed by remember { mutableStateOf(false) }
    suspend fun load() {
        loadFailed = false
        docs = null
        val fetched = client.fetchTopicDocs()
        if (fetched == null) loadFailed = true else docs = fetched
    }
    LaunchedEffect(Unit) { load() }
    Column(Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "토픽별 주입 문서",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = { haptics.tap(); onOpenTopicDoc("") }) { Text("+ 새 문서") }
        }
        TopicDocsList(docs, loadFailed, onRetry = { scope.launch { load() } }, onOpenTopicDoc = onOpenTopicDoc)
    }
}

@Composable
private fun TopicDocsList(
    list: List<TopicDocFile>?,
    loadFailed: Boolean,
    onRetry: () -> Unit,
    onOpenTopicDoc: (String) -> Unit,
) {
    val haptics = rememberHaptics()
    when {
        loadFailed -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            DenebError("토픽 문서를 불러오지 못했습니다.", onRetry = onRetry)
        }
        list == null -> DenebLoading()
        list.isEmpty() -> EmptyTab("토픽 문서가 없습니다.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(list, key = { it.name }) { doc ->
                Row(
                    modifier = Modifier.animateItem().fillMaxWidth().clickable { haptics.tap(); onOpenTopicDoc(doc.name) }.padding(horizontal = 16.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        doc.name,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (doc.modified.isNotBlank()) {
                        Text(doc.modified.take(10), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp), color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.4f))
            }
        }
    }
}
