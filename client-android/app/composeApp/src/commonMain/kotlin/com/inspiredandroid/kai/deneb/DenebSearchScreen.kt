package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Unified discovery (`miniapp.search.all`): one box searches wiki, diary and
 * people. Wiki hits open the page view; people/diary show inline. Surface-
 * wrapped so all text is visible in dark mode.
 */
@Composable
fun DenebSearchScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var query by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<SearchResults?>(null) }
    var searching by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun run() {
        val q = query.trim()
        if (q.isEmpty() || searching) return
        scope.launch {
            searching = true
            results = client.searchAll(q)
            searching = false
        }
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.statusBarsPadding().padding(16.dp).verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(16.dp))
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    "검색",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = { onOpenWiki("") }) { Text("+ 새 위키") }
                TextButton(onClick = onBack) { Text("닫기") }
            }
            Spacer(Modifier.height(8.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    placeholder = { Text("위키 · 일기 · 사람 검색") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    keyboardActions = KeyboardActions(onSearch = { run() }),
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(8.dp))
                TextButton(onClick = { run() }, enabled = !searching) { Text(if (searching) "…" else "검색") }
            }
            Spacer(Modifier.height(12.dp))

            val r = results
            when {
                searching && r == null -> DenebLoading("검색 중…")
                r == null -> Text(
                    "위키 · 일기 · 사람을 한 번에 검색합니다.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                r.wiki.isEmpty() && r.diary.isEmpty() && r.people.isEmpty() -> Text(
                    "결과 없음",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                else -> {
                    if (r.wiki.isNotEmpty()) {
                        GroupHeader("위키 ${r.wiki.size}")
                        r.wiki.forEach { hit ->
                            ResultRow(hit.title, hit.snippet) { onOpenWiki(hit.path) }
                        }
                    }
                    if (r.people.isNotEmpty()) {
                        GroupHeader("사람 ${r.people.size}")
                        r.people.forEach { person ->
                            ResultRow(
                                "${person.name}  ·  ${person.messageCount}통",
                                person.lastSubject,
                                onClick = null,
                            )
                        }
                    }
                    if (r.diary.isNotEmpty()) {
                        GroupHeader("일기 ${r.diary.size}")
                        r.diary.forEach { hit ->
                            ResultRow(hit.title, hit.snippet, onClick = null)
                        }
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }
        }
    }
}

@Composable
private fun GroupHeader(label: String) {
    Spacer(Modifier.height(12.dp))
    Text(
        label,
        style = MaterialTheme.typography.titleSmall,
        color = MaterialTheme.colorScheme.primary,
    )
    Spacer(Modifier.height(2.dp))
}

@Composable
private fun ResultRow(title: String, snippet: String, onClick: (() -> Unit)?) {
    Column(
        Modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable { onClick() } else Modifier)
            .padding(vertical = 10.dp),
    ) {
        Text(
            title.ifBlank { "(제목 없음)" },
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (snippet.isNotBlank()) {
            Text(
                snippet,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
