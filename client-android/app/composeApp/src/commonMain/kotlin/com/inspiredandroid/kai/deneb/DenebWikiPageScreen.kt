package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Wiki/memory page surface (`miniapp.memory.*`). Blank [path] enters create
 * mode (title + category + body -> create_page); otherwise reads the page with
 * a markdown view / body edit toggle. write_page preserves frontmatter, so the
 * editor only exposes the body.
 */
@Composable
fun DenebWikiPageScreen(
    client: DenebGatewayClient,
    path: String,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val creating = path.isBlank()
    var page by remember(path) { mutableStateOf<WikiPage?>(null) }
    var loadFailed by remember(path) { mutableStateOf(false) }
    var editing by remember(path) { mutableStateOf(creating) }
    var draftTitle by remember(path) { mutableStateOf("") }
    var draftCategory by remember(path) { mutableStateOf("") }
    var draftSummary by remember(path) { mutableStateOf("") }
    var draftTags by remember(path) { mutableStateOf("") }
    var draftBody by remember(path) { mutableStateOf("") }
    var saving by remember(path) { mutableStateOf(false) }
    var status by remember(path) { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(path) {
        if (!creating) {
            val p = client.fetchWikiPage(path)
            page = p
            loadFailed = p == null
            if (p != null) draftBody = p.body
        }
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.statusBarsPadding().padding(16.dp).verticalScroll(rememberScrollState()),
        ) {
            if (navigationTabBar != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { navigationTabBar() }
                Spacer(Modifier.height(12.dp))
            }
            TextButton(onClick = onBack) { Text("← 뒤로") }
            Spacer(Modifier.height(4.dp))

            val pg = page
            if (creating) {
                Text(
                    "새 위키 페이지",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = draftTitle,
                    onValueChange = { draftTitle = it },
                    label = { Text("제목") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = draftCategory,
                    onValueChange = { draftCategory = it },
                    label = { Text("카테고리 (예: people, projects)") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            } else if (pg == null) {
                if (loadFailed) DenebError("페이지를 불러오지 못했습니다.") else DenebLoading()
                return@Column
            } else {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        pg.title,
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = {
                        if (!editing) {
                            draftTitle = pg.title
                            draftSummary = pg.summary
                            draftTags = pg.tags.joinToString(", ")
                            draftBody = pg.body
                        }
                        editing = !editing
                        status = null
                    }) {
                        Text(if (editing) "취소" else "편집")
                    }
                }
                val meta = buildList {
                    if (pg.category.isNotBlank()) add(pg.category)
                    if (pg.updated.isNotBlank()) add(pg.updated.take(10))
                }.joinToString("  ·  ")
                if (meta.isNotBlank()) {
                    Text(meta, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (pg.tags.isNotEmpty()) {
                    Text(
                        pg.tags.joinToString(" ") { "#$it" },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
            }

            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(12.dp))

            if (editing) {
                if (!creating) {
                    OutlinedTextField(draftTitle, { draftTitle = it }, label = { Text("제목") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    Spacer(Modifier.height(8.dp))
                    OutlinedTextField(draftSummary, { draftSummary = it }, label = { Text("요약") }, modifier = Modifier.fillMaxWidth())
                    Spacer(Modifier.height(8.dp))
                    OutlinedTextField(draftTags, { draftTags = it }, label = { Text("태그 (쉼표로 구분)") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    Spacer(Modifier.height(8.dp))
                }
                OutlinedTextField(
                    value = draftBody,
                    onValueChange = { draftBody = it },
                    label = { Text("본문 (마크다운)") },
                    modifier = Modifier.fillMaxWidth().height(360.dp),
                )
                Spacer(Modifier.height(12.dp))
                Button(
                    enabled = !saving && (!creating || draftTitle.isNotBlank()),
                    onClick = {
                        scope.launch {
                            saving = true
                            status = null
                            if (creating) {
                                val newPath = client.createWikiPage(draftTitle.trim(), draftCategory.trim(), draftBody)
                                saving = false
                                if (newPath != null) onBack() else status = "생성 실패"
                            } else {
                                val tags = draftTags.split(",").map { it.trim() }.filter { it.isNotBlank() }
                                val ok = client.saveWikiPage(path, draftBody, draftTitle.trim(), draftSummary.trim(), tags)
                                saving = false
                                if (ok) {
                                    editing = false
                                    status = "저장됨"
                                    page = client.fetchWikiPage(path)
                                } else {
                                    status = "저장 실패"
                                }
                            }
                        }
                    },
                ) { Text(if (saving) "저장 중…" else if (creating) "생성" else "저장") }
                status?.let {
                    Spacer(Modifier.height(8.dp))
                    Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            } else if (pg != null) {
                DenebMarkdown(pg.body.ifBlank { "(빈 페이지)" })
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}
