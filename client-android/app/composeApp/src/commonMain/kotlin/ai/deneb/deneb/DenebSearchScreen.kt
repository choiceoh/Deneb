package ai.deneb.deneb

import ai.deneb.Platform
import ai.deneb.currentPlatform
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * Unified discovery (`miniapp.search.all`): one box searches wiki, diary and
 * people. Wiki hits open the page view; people/diary show inline. Framed by
 * [DenebScreenScaffold].
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

    // Desktop: the persistent sidebar is the navigation — a back affordance on a
    // top-level section is redundant there (showBack drops it).
    DenebScreenScaffold(
        title = "검색",
        onBack = onBack,
        tabBar = navigationTabBar,
        showBack = currentPlatform !is Platform.Desktop,
    ) {
        Column(
            // The scaffold's imePadding shrinks this weighted column above the soft
            // keyboard, so the bottom search results stay reachable instead of
            // hiding behind it (edge-to-edge: the app owns the IME inset).
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp),
        ) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onOpenCategories) { Text("카테고리") }
                TextButton(onClick = { onOpenWiki("") }) { Text("+ 새 위키") }
            }
            Spacer(Modifier.height(4.dp))
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

                failed -> DenebError(
                    "검색에 실패했습니다.",
                    onRetry = { run() },
                )

                r == null -> Text(
                    "위키 · 일기 · 사람을 한 번에 검색합니다.",
                    style = DenebType.body,
                    color = denebHint(),
                )

                r.wiki.isEmpty() && r.diary.isEmpty() && r.people.isEmpty() -> DenebEmpty("검색 결과가 없습니다")

                else -> {
                    if (r.wiki.isNotEmpty()) {
                        GroupHeader("위키", r.wiki.size)
                        r.wiki.forEach { hit ->
                            ResultRow(hit.title, hit.snippet) { onOpenWiki(hit.path) }
                        }
                    }
                    if (r.people.isNotEmpty()) {
                        GroupHeader("사람", r.people.size)
                        r.people.forEach { person ->
                            ResultRow(
                                person.name,
                                person.lastSubject,
                                // Message volume is the activity signal for this person —
                                // the one cool-primary mark on the row (matches DenebPeopleScreen).
                                trailing = "${person.messageCount}통",
                                onClick = { onOpenPerson(person.email.ifBlank { person.name }) },
                            )
                        }
                    }
                    if (r.diary.isNotEmpty()) {
                        GroupHeader("일기", r.diary.size)
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
