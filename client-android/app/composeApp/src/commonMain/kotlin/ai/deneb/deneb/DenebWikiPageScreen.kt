package ai.deneb.deneb

import ai.deneb.deneb.generated.NotebookSummaryOut
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
    onOpenNotebook: (String) -> Unit = {}, // opens this deal's raw-evidence notebook (project pages)
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
    // The deal notebook (raw evidence) for a project page: matched by the page's
    // frozen code against notebooks' dealRef. null when this page has no code or
    // no notebook exists yet.
    var dealNotebook by remember(path) { mutableStateOf<NotebookSummaryOut?>(null) }
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()

    suspend fun loadPage() {
        if (creating) return
        loadFailed = false
        page = null
        val p = client.fetchWikiPage(path)
        page = p
        loadFailed = p == null
        if (p != null) draftBody = p.body
    }
    LaunchedEffect(path) { loadPage() }
    // Resolve the deal notebook once the page (and its code) is known. Re-runs when
    // code goes blank → loaded. Curated facts (this page) ↔ raw evidence (notebook),
    // joined by the shared project code.
    LaunchedEffect(page?.code) {
        val code = page?.code?.trim().orEmpty()
        dealNotebook = if (code.isBlank()) {
            null
        } else {
            client.fetchNotebooks()?.firstOrNull { it.dealRef.trim().equals(code, ignoreCase = true) }
        }
    }

    // Guard against losing in-progress wiki edits to a stray back: while editing,
    // snapshot the drafts (captured when edit mode opens) and confirm before leaving
    // if they changed. Viewing (not editing) leaves immediately.
    val snapshot = listOf<Any?>(draftTitle, draftCategory, draftSummary, draftTags, draftBody)
    val baseline = remember(editing, page) { if (editing) snapshot else null }
    val requestBack = rememberDiscardGuard(editing && baseline != null && snapshot != baseline, onBack)

    DenebScreenScaffold(title = "위키", onBack = requestBack, tabBar = navigationTabBar) {
        Column(
            // The scaffold's imePadding shrinks this weighted column above the soft
            // keyboard, so the edit fields (title/summary/tags/body) stay reachable
            // (edge-to-edge: the app owns the IME inset).
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Spacer(Modifier.height(8.dp))

            val pg = page
            if (creating) {
                Text(
                    "새 위키 페이지",
                    style = DenebType.subject,
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
                if (loadFailed) {
                    DenebError("페이지를 불러오지 못했습니다.", onRetry = { scope.launch { loadPage() } })
                } else {
                    DenebLoading()
                }
                return@Column
            } else {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        pg.title,
                        style = DenebType.subject,
                        color = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = {
                        haptics.toggle(!editing)
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
                    Text(meta, style = DenebType.meta, color = denebHint())
                }
                if (pg.tags.isNotEmpty()) {
                    Text(
                        pg.tags.joinToString(" ") { "#$it" },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
                // Jump to this deal's raw-evidence notebook (project pages only).
                // The page is the curated facts; the notebook is the pinned sources
                // behind them — same project code, two faces.
                val nb = dealNotebook
                if (nb != null) {
                    Spacer(Modifier.height(10.dp))
                    DealNotebookLinkRow(sourceCount = nb.sourceCount) {
                        haptics.tap()
                        onOpenNotebook(nb.id)
                    }
                }
            }

            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = denebHairline())
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
                        haptics.confirm()
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
                ) {
                    Text(
                        if (saving) {
                            "저장 중…"
                        } else if (creating) {
                            "생성"
                        } else {
                            "저장"
                        },
                    )
                }
                status?.let {
                    Spacer(Modifier.height(8.dp))
                    Text(it, style = DenebType.meta, color = denebHint())
                }
            } else if (pg != null) {
                MarkdownContent(pg.body.ifBlank { "(빈 페이지)" }, baseStyle = MaterialTheme.typography.bodyMedium)
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}

/**
 * The "이 딜 노트북" link shown on a project wiki page — taps through to the deal's
 * raw-evidence notebook. The page is the curated facts; the notebook is the pinned
 * sources behind them, joined by the shared project code. Stateless (source count +
 * click callback) so the render harness can show it; uses the flat "label + →" row
 * idiom of the category browser's pinned entries.
 */
@Composable
internal fun DealNotebookLinkRow(sourceCount: Int, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text("이 딜 노트북", style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onSurface)
            Text("자료 ${sourceCount}건 · raw 증거", style = DenebType.meta, color = denebHint())
        }
        Text("→", style = DenebType.meta, color = MaterialTheme.colorScheme.primary)
    }
}
