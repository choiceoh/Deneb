package ai.deneb.deneb

import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebUnderlineSearchField
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

private const val SEARCH_QUERY_MAX_CODE_POINTS = 500

// Keep unified-search file opens aligned with Andromeda's 2 MiB pre-download gate.
internal const val SEARCH_FILE_DIRECT_OPEN_MAX_BYTES = 2L * 1024 * 1024

internal data class SearchRunState(
    val generation: Long = 0L,
    val submittedQuery: String = "",
    val searching: Boolean = false,
    val failed: Boolean = false,
    val results: SearchResults? = null,
) {
    fun starting(query: String): SearchRunState = SearchRunState(
        generation = generation + 1,
        submittedQuery = query,
        searching = true,
    )

    fun queryEdited(currentQuery: String): SearchRunState {
        if (submittedQuery.isEmpty() || currentQuery.trim() == submittedQuery) return this
        return SearchRunState(generation = generation + 1)
    }

    fun completed(requestGeneration: Long, requestQuery: String, next: SearchResults?): SearchRunState {
        if (!searching || generation != requestGeneration || submittedQuery != requestQuery) return this
        return copy(searching = false, failed = next == null, results = next)
    }
}

/** Inline fact-detail state. A new tap clears the previous payload before the
 *  network request and advances [generation], so a corrected/forgotten fact's
 *  late response cannot reappear after another tap or query edit. */
internal data class CurrentFactDetailState(
    val generation: Long = 0L,
    val path: String = "",
    val factId: String = "",
    val subjectId: String = "",
    val loading: Boolean = false,
    val newSearchRequired: Boolean = false,
    val detail: CurrentFactDetail? = null,
) {
    fun starting(hit: SearchHit): CurrentFactDetailState = CurrentFactDetailState(
        generation = generation + 1,
        path = hit.path,
        factId = hit.factId,
        subjectId = hit.subjectId,
        loading = true,
    )

    fun cleared(): CurrentFactDetailState = CurrentFactDetailState(generation = generation + 1)

    fun completed(requestGeneration: Long, requestPath: String, next: CurrentFactDetail?): CurrentFactDetailState {
        if (!loading || generation != requestGeneration || path != requestPath) return this
        return copy(loading = false, newSearchRequired = next == null, detail = next)
    }
}

internal fun boundSearchQuery(value: String): String {
    var codePoints = 0
    var end = 0
    while (end < value.length && codePoints < SEARCH_QUERY_MAX_CODE_POINTS) {
        val char = value[end]
        end += if (char.isHighSurrogate() && end + 1 < value.length && value[end + 1].isLowSurrogate()) 2 else 1
        codePoints++
    }
    return value.substring(0, end)
}

internal fun searchFileNeedsDownloadConfirmation(hit: SearchFileResult): Boolean = hit.size > SEARCH_FILE_DIRECT_OPEN_MAX_BYTES

/** The single click-routing boundary for wiki-group results. Fact hits remain
 *  inline; only ordinary pages may reach editable wiki navigation. */
internal fun openSearchWikiHit(
    hit: SearchHit,
    onOpenWiki: (String) -> Unit,
    onOpenFact: (SearchHit) -> Unit,
) {
    if (hit.isCurrentFactHit()) onOpenFact(hit) else onOpenWiki(hit.path)
}

/**
 * Unified discovery (`miniapp.search.all`): one box searches wiki, diary,
 * people, files and mail. Every online source reports its own availability so
 * a backend failure is never rendered as a trustworthy empty result. Framed by
 * [DenebScreenScaffold]. The stateful shell owns the query/results; [SearchContent]
 * is the stateless body (renderPreviews-friendly).
 */
@Composable
fun DenebSearchScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    onOpenPerson: (String) -> Unit = {},
    onOpenFile: (SearchFileResult) -> Unit = {},
    onOpenMail: (String) -> Unit = {},
    onOpenCategories: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var query by remember { mutableStateOf("") }
    var runState by remember { mutableStateOf(SearchRunState()) }
    var factDetailState by remember { mutableStateOf(CurrentFactDetailState()) }
    var pendingFileOpen by remember { mutableStateOf<SearchFileResult?>(null) }
    val scope = rememberCoroutineScope()

    fun run() {
        val q = boundSearchQuery(query.trim())
        if (q.isEmpty()) return
        // Keep one request for an unchanged query. Editing invalidates that generation and permits a replacement.
        if (runState.searching) return
        // Clear the previous query's evidence before the next network request.
        // Otherwise a slow search can pair a new query with stale provenance.
        val started = runState.starting(q)
        runState = started
        factDetailState = factDetailState.cleared()
        scope.launch {
            val r = client.searchAll(q)
            runState = runState.completed(started.generation, q, r)
        }
    }

    DenebScreenScaffold(
        title = "검색",
        onBack = onBack,
        tabBar = navigationTabBar,
    ) {
        SearchContent(
            modifier = Modifier.weight(1f),
            query = query,
            onQueryChange = { value ->
                val bounded = boundSearchQuery(value)
                if (bounded != query) factDetailState = factDetailState.cleared()
                query = bounded
                runState = runState.queryEdited(bounded)
            },
            onSearch = { run() },
            searching = runState.searching,
            failed = runState.failed,
            results = runState.results,
            onOpenWiki = onOpenWiki,
            onOpenPerson = onOpenPerson,
            onOpenFile = { hit ->
                if (searchFileNeedsDownloadConfirmation(hit)) {
                    pendingFileOpen = hit
                } else {
                    onOpenFile(hit)
                }
            },
            onOpenMail = onOpenMail,
            onOpenCategories = onOpenCategories,
            factDetailState = factDetailState,
            onOpenFact = { hit ->
                // Do not toggle a cached detail. Every tap revalidates this
                // immutable claim ID against the canonical fact plane.
                val started = factDetailState.starting(hit)
                factDetailState = started
                scope.launch {
                    val detail = client.fetchCurrentFactPage(hit.path)
                    factDetailState = factDetailState.completed(started.generation, hit.path, detail)
                }
            },
        )
    }

    pendingFileOpen?.let { hit ->
        val displayName = hit.name.ifBlank { hit.path }
        AlertDialog(
            onDismissRequest = { pendingFileOpen = null },
            title = { Text("큰 파일 다운로드") },
            text = {
                Text(
                    "$displayName · ${humanBytes(hit.size)}\n\n" +
                        "${humanBytes(SEARCH_FILE_DIRECT_OPEN_MAX_BYTES)}보다 큰 파일입니다. " +
                        "외부 브라우저에서 다운로드할까요?",
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        pendingFileOpen = null
                        onOpenFile(hit)
                    },
                ) { Text("다운로드") }
            },
            dismissButton = {
                TextButton(onClick = { pendingFileOpen = null }) { Text("취소") }
            },
        )
    }
}

@Composable
internal fun SearchContent(
    modifier: Modifier,
    query: String,
    onQueryChange: (String) -> Unit,
    onSearch: () -> Unit,
    searching: Boolean,
    failed: Boolean,
    results: SearchResults?,
    onOpenWiki: (String) -> Unit,
    onOpenPerson: (String) -> Unit,
    onOpenFile: (SearchFileResult) -> Unit = {},
    onOpenMail: (String) -> Unit = {},
    onOpenCategories: () -> Unit,
    factDetailState: CurrentFactDetailState = CurrentFactDetailState(),
    onOpenFact: (SearchHit) -> Unit = {},
) {
    Column(
        // The scaffold's imePadding shrinks this weighted column above the soft
        // keyboard, so the bottom results stay reachable instead of hiding behind
        // it (edge-to-edge: the app owns the IME inset).
        modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
    ) {
        Spacer(Modifier.height(8.dp))
        DenebUnderlineSearchField(
            query = query,
            onQueryChange = onQueryChange,
            placeholder = "위키 · 파일 · 메일 · 일기 · 사람",
            onSearch = onSearch,
        )
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            TextButton(onClick = onOpenCategories) {
                Text("카테고리", style = DenebType.hint)
            }
            TextButton(onClick = { onOpenWiki("") }) {
                Text("새 위키", style = DenebType.hint)
            }
        }
        Spacer(Modifier.height(8.dp))

        val r = results
        when {
            searching && r == null -> DenebLoading("검색 중…")

            failed -> DenebError("검색에 실패했습니다.", onRetry = onSearch)

            // Single-user product: no "여기서 …를 검색합니다" hand-holding. The placeholder
            // already says what's searchable, so the initial state is just the field.
            r == null -> Unit

            r.wiki.isEmpty() && r.diary.isEmpty() && r.people.isEmpty() && r.files.isEmpty() && r.mail.isEmpty() -> {
                SearchSourceNotices(r.sourceStatus)
                DenebEmpty("검색 결과가 없습니다", icon = Icons.Outlined.Search, hint = "개인 자료 전체에서 다시 검색해 보세요")
            }

            else -> {
                SearchSourceNotices(r.sourceStatus)
                if (r.wiki.isNotEmpty()) {
                    GroupHeader("위키", r.wiki.size)
                    r.wiki.forEach { hit ->
                        val isFact = hit.isCurrentFactHit()
                        ResultRow(
                            title = hit.title,
                            snippet = if (isFact) hit.subjectId else hit.snippet,
                            trailing = if (isFact) "읽기 전용" else null,
                            onClick = { openSearchWikiHit(hit, onOpenWiki, onOpenFact) },
                        )
                        if (isFact && factDetailState.path == hit.path) {
                            CurrentFactDetailPanel(factDetailState)
                        }
                    }
                }
                if (r.files.isNotEmpty()) {
                    GroupHeader("파일", r.files.size)
                    r.files.forEach { hit ->
                        val locator = searchFileLocator(hit)
                        val snippet = listOf(locator, hit.snippet).filter { it.isNotBlank() }.joinToString(" · ")
                        ResultRow(
                            hit.name.ifBlank { hit.path },
                            snippet,
                            trailing = hit.kind.ifBlank { null },
                            onClick = { onOpenFile(hit) },
                        )
                    }
                }
                if (r.mail.isNotEmpty()) {
                    GroupHeader("메일", r.mail.size)
                    r.mail.forEach { hit ->
                        val snippet = listOf(hit.from, hit.date, hit.snippet).filter { it.isNotBlank() }.joinToString(" · ")
                        ResultRow(
                            hit.subject.ifBlank { "(제목 없음)" },
                            snippet,
                            trailing = hit.mailbox.ifBlank { null },
                            onClick = { onOpenMail(hit.id) },
                        )
                    }
                }
                if (r.people.isNotEmpty()) {
                    GroupHeader("사람", r.people.size)
                    r.people.forEach { person ->
                        ResultRow(
                            person.name,
                            person.lastSubject,
                            // Message volume is the activity signal — the one cool-primary
                            // mark on the row (matches DenebPeopleScreen).
                            trailing = "${person.messageCount}통",
                            onClick = { onOpenPerson(person.email.ifBlank { person.name }) },
                        )
                    }
                }
                if (r.diary.isNotEmpty()) {
                    GroupHeader("일기", r.diary.size)
                    r.diary.forEach { hit ->
                        ResultRow(
                            hit.title,
                            hit.snippet,
                            onClick = hit.path.takeIf { it.isNotBlank() }?.let { path -> { onOpenWiki(path) } },
                        )
                    }
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

@Composable
private fun CurrentFactDetailPanel(state: CurrentFactDetailState) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 8.dp, top = 8.dp, bottom = 12.dp),
    ) {
        Text(
            "현재 사실 · 읽기 전용",
            style = DenebType.sectionLabel,
            color = MaterialTheme.colorScheme.primary,
        )
        Spacer(Modifier.height(8.dp))
        when {
            state.loading -> Text(
                "최신 상태 확인 중…",
                style = DenebType.meta,
                color = denebHint(),
            )

            state.newSearchRequired -> Text(
                "이 사실 참조는 더 이상 최신이 아니거나 확인할 수 없습니다. 다시 검색해 주세요.",
                style = DenebType.body,
                color = denebHint(),
            )

            state.detail != null -> {
                Text(
                    state.detail.title,
                    style = DenebType.rowTitleStrong,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                if (state.detail.summary.isNotBlank()) {
                    Spacer(Modifier.height(4.dp))
                    Text(
                        state.detail.summary,
                        style = DenebType.meta,
                        color = denebHint(),
                    )
                }
                Spacer(Modifier.height(8.dp))
                MarkdownContent(
                    content = state.detail.body.ifBlank { "(내용 없음)" },
                    isInteractive = false,
                    baseStyle = DenebType.body,
                )
            }
        }
        Spacer(Modifier.height(8.dp))
        HorizontalDivider(color = denebHairline(), thickness = 0.5.dp)
    }
}

@Composable
private fun SearchSourceNotices(status: SearchSourceAvailability) {
    val notices = searchSourceNotices(status)
    if (notices.isEmpty()) return
    Column(Modifier.fillMaxWidth().padding(top = 12.dp)) {
        notices.forEach { notice ->
            Text(
                text = notice,
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(bottom = 3.dp),
            )
        }
    }
}

internal fun searchSourceNotices(status: SearchSourceAvailability): List<String> = listOf(
    "위키" to status.wiki,
    "일기" to status.diary,
    "사람" to status.people,
    "파일" to status.files,
    "메일" to status.mail,
).mapNotNull { (label, raw) ->
    when (raw.lowercase()) {
        "", "ok" -> null
        "partial" -> "$label · 일부 결과만 표시"
        "offline" -> "$label · 기기 저장본"
        "timeout" -> "$label · 시간 초과"
        "error" -> "$label · 검색 실패"
        "unavailable" -> "$label · 현재 사용할 수 없음"
        else -> "$label · 상태 확인 필요"
    }
}

internal fun searchFileLocator(hit: SearchFileResult): String = buildList {
    if (hit.heading.isNotBlank()) add(hit.heading)
    when {
        hit.startLine > 0 && hit.endLine > hit.startLine -> add("${hit.startLine}–${hit.endLine}행")
        hit.startLine > 0 -> add("${hit.startLine}행")
    }
}.joinToString(" · ")

@Composable
private fun GroupHeader(label: String, count: Int) {
    // Tracked-caps grouping label with the hit count as the one cool-primary mark —
    // a small interactive indicator, not a colored row (design refresh §accents).
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.padding(top = 22.dp, bottom = 8.dp),
    ) {
        Text(
            text = label.uppercase(),
            style = DenebType.sectionLabel,
            color = denebHint(),
        )
        Spacer(Modifier.width(6.dp))
        Text(
            text = count.toString(),
            style = DenebType.sectionLabel,
            color = MaterialTheme.colorScheme.primary,
        )
    }
}

@Composable
private fun ResultRow(title: String, snippet: String, trailing: String? = null, onClick: (() -> Unit)?) {
    val haptics = rememberHaptics()
    DenebRow(
        onClick = onClick?.let { cb ->
            {
                haptics.tap()
                cb()
            }
        },
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                title.ifBlank { "(제목 없음)" },
                style = DenebType.subject,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (trailing != null) {
                Spacer(Modifier.width(8.dp))
                Text(
                    trailing,
                    style = DenebType.meta,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
        if (snippet.isNotBlank()) {
            Text(
                snippet,
                style = DenebType.snippet,
                color = denebHint(),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
