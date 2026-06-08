package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DenebRow
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebSectionLabel
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.denebHairline
import com.inspiredandroid.kai.ui.denebHint
import com.inspiredandroid.kai.ui.components.rememberHaptics
import kotlinx.coroutines.launch

/**
 * Unified discovery (`miniapp.search.all`): one box searches wiki, diary and people.
 * Wiki hits open the page view; people open the person view; diary shows inline.
 *
 * Design (.claude/rules/native-design-system.md): the Deneb skin — [DenebScreenScaffold]
 * frame, a flat hairline-underlined search input (no Material box), and [DenebRow] result
 * rows in [DenebType]. Body rendering lives in [SearchContent] so the render harness can
 * preview it; this is the stateful shell (query + search RPC).
 */
@Composable
fun DenebSearchScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenWiki: (String) -> Unit = {},
    onOpenPerson: (String) -> Unit = {},
    onOpenCategories: () -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var query by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<SearchResults?>(null) }
    var searching by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun run() {
        val q = query.trim()
        if (q.isEmpty() || searching) return
        scope.launch {
            searching = true
            failed = false
            val r = client.searchAll(q)
            failed = r == null
            results = r
            searching = false
        }
    }

    DenebScreenScaffold(title = "검색", onBack = onBack, tabBar = navigationTabBar) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp),
        ) {
            SearchContent(
                query = query,
                onQuery = { query = it },
                onSearch = { run() },
                searching = searching,
                failed = failed,
                results = results,
                onOpenWiki = onOpenWiki,
                onOpenPerson = onOpenPerson,
                onOpenCategories = onOpenCategories,
            )
        }
    }
}

/**
 * Stateless search body — extracted so [RenderPreview] can render it with mock results.
 * Pure presentation: a flat search field, quick actions, and hairline result rows.
 */
@Composable
internal fun SearchContent(
    query: String,
    onQuery: (String) -> Unit,
    onSearch: () -> Unit,
    searching: Boolean,
    failed: Boolean,
    results: SearchResults?,
    onOpenWiki: (String) -> Unit,
    onOpenPerson: (String) -> Unit,
    onOpenCategories: () -> Unit,
) {
    Spacer(Modifier.height(8.dp))
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        BasicTextField(
            value = query,
            onValueChange = onQuery,
            textStyle = DenebType.body.copy(color = MaterialTheme.colorScheme.onBackground),
            cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { onSearch() }),
            modifier = Modifier.weight(1f),
            decorationBox = { inner ->
                if (query.isEmpty()) {
                    Text("위키 · 일기 · 사람 검색", style = DenebType.body, color = denebHint())
                }
                inner()
            },
        )
        TextButton(onClick = onSearch, enabled = !searching) {
            Text(if (searching) "…" else "검색")
        }
    }
    Spacer(Modifier.height(8.dp))
    HorizontalDivider(color = denebHairline())

    Spacer(Modifier.height(4.dp))
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        TextButton(onClick = onOpenCategories) { Text("카테고리") }
        TextButton(onClick = { onOpenWiki("") }) { Text("새 위키") }
    }

    Spacer(Modifier.height(8.dp))
    val r = results
    when {
        searching && r == null -> DenebLoading("검색 중…")
        failed -> DenebError("검색에 실패했어요.", onRetry = onSearch)
        r == null -> Text(
            "위키 · 일기 · 사람을 한 번에 검색합니다.",
            style = DenebType.body,
            color = denebHint(),
        )
        r.wiki.isEmpty() && r.diary.isEmpty() && r.people.isEmpty() -> DenebEmpty("결과 없음")
        else -> {
            if (r.wiki.isNotEmpty()) {
                DenebSectionLabel("위키 ${r.wiki.size}")
                r.wiki.forEach { hit ->
                    SearchResultRow(hit.title, hit.snippet) { onOpenWiki(hit.path) }
                }
            }
            if (r.people.isNotEmpty()) {
                DenebSectionLabel("사람 ${r.people.size}")
                r.people.forEach { person ->
                    SearchResultRow(
                        "${person.name}  ·  ${person.messageCount}통",
                        person.lastSubject,
                    ) { onOpenPerson(person.email.ifBlank { person.name }) }
                }
            }
            if (r.diary.isNotEmpty()) {
                DenebSectionLabel("일기 ${r.diary.size}")
                r.diary.forEach { hit ->
                    SearchResultRow(hit.title, hit.snippet, null)
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}

@Composable
private fun SearchResultRow(title: String, snippet: String, onClick: (() -> Unit)?) {
    val haptics = rememberHaptics()
    DenebRow(onClick = onClick?.let { cb -> { haptics.tap(); cb() } }) {
        Text(
            title.ifBlank { "(제목 없음)" },
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (snippet.isNotBlank()) {
            Spacer(Modifier.height(3.dp))
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
