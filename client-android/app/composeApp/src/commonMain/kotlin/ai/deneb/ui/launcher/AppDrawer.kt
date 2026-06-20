package ai.deneb.ui.launcher

import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebLoading
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSearchField
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * One launchable app for the work-launcher app drawer. [icon] is unused by the
 * text-first drawer (kept for source compatibility; the Niagara-style list shows
 * labels, not icons) and is null on platforms without an installed-app list.
 */
data class LauncherAppEntry(
    val label: String,
    val packageName: String,
    val icon: ImageBitmap? = null,
)

/**
 * The work launcher's app drawer — a Niagara-style alphabetical TEXT list with a
 * ㄱㄴㄷ/A–Z scrub index, in the Deneb idiom (flat AMOLED, monochrome, no icon grid:
 * colorful app icons were the one off-brand element). Pure presentation — the
 * platform supplies [apps] (Android = PackageManager; desktop/iOS = stub) and
 * [onLaunch] fires the launch intent. Offline-first shell: never touches the
 * gateway, so the home can reach other apps even with the server down.
 */
@Composable
fun AppDrawer(
    apps: List<LauncherAppEntry>,
    onLaunch: (String) -> Unit,
    modifier: Modifier = Modifier,
    loaded: Boolean = true,
) {
    var query by remember { mutableStateOf("") }
    val filtered = remember(apps, query) {
        val q = query.trim()
        if (q.isEmpty()) apps else apps.filter { it.label.contains(q, ignoreCase = true) }
    }
    Column(modifier.fillMaxSize()) {
        DenebSearchField(
            query = query,
            onQueryChange = { query = it },
            placeholder = "앱 검색",
            clearContentDescription = "검색 지우기",
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            // Launcher idiom: when the query narrows to a single app, the keyboard's
            // Search/Enter launches it directly — type a few letters, hit enter, gone.
            onSearch = { filtered.singleOrNull()?.let { onLaunch(it.packageName) } },
        )
        when {
            // Distinct loading vs empty: the provider loads off-thread, so without this
            // the drawer flashed "앱 없음" on every open before the list populated.
            !loaded -> DenebLoading()

            filtered.isEmpty() -> DenebEmpty(if (apps.isEmpty()) "앱이 없습니다" else "검색 결과 없음")

            else -> AppList(filtered, onLaunch)
        }
    }
}

/** Apps grouped under one initial (ㄱ/ㄴ/… or A/B/… or #), label-sorted. */
private data class AppSection(val key: String, val apps: List<LauncherAppEntry>)

@Composable
private fun AppList(apps: List<LauncherAppEntry>, onLaunch: (String) -> Unit) {
    val sections = remember(apps) { sectionsOf(apps) }
    // Flat LazyColumn index of each section header, so the scrub can scrollToItem.
    val headerIndex = remember(sections) {
        buildMap {
            var idx = 0
            sections.forEach {
                put(it.key, idx)
                idx += 1 + it.apps.size
            }
        }
    }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    Box(Modifier.fillMaxSize()) {
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            // Leave room on the right for the scrub index.
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp, end = 24.dp),
        ) {
            sections.forEach { sec ->
                item(key = "h:${sec.key}") { SectionHeader(sec.key) }
                items(sec.apps, key = { it.packageName }) { app -> AppRow(app, onLaunch) }
            }
        }
        // Scrub index: drag over the letters to jump to that section (Niagara idiom).
        // Hidden when there's only one section (nothing to scrub between).
        if (sections.size > 1) {
            ScrubIndex(
                keys = sections.map { it.key },
                onScrub = { key -> headerIndex[key]?.let { i -> scope.launch { listState.scrollToItem(i) } } },
                modifier = Modifier.align(Alignment.CenterEnd).fillMaxHeight().width(22.dp).padding(vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun SectionHeader(key: String) {
    Text(
        key,
        style = DenebType.sectionLabel,
        color = denebHint(),
        modifier = Modifier.padding(start = 20.dp, end = 20.dp, top = 10.dp, bottom = 2.dp),
    )
}

@Composable
private fun AppRow(app: LauncherAppEntry, onLaunch: (String) -> Unit) {
    Text(
        app.label,
        style = DenebType.rowTitle,
        color = MaterialTheme.colorScheme.onBackground,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier
            .fillMaxWidth()
            .denebPressable(onClick = { onLaunch(app.packageName) })
            .padding(horizontal = 20.dp, vertical = 11.dp),
    )
}

@Composable
private fun ScrubIndex(keys: List<String>, onScrub: (String) -> Unit, modifier: Modifier = Modifier) {
    Column(
        // Touch-and-slide over the column maps Y → section key and fires onScrub
        // continuously, so a single drag flies down the alphabet (Niagara idiom).
        modifier.pointerInput(keys) {
            awaitEachGesture {
                val down = awaitFirstDown(requireUnconsumed = false)
                // Dedup within a drag: fire scrollToItem only when the target section
                // changes, not on every pointer frame (each was a fresh coroutine).
                var lastKey: String? = null
                fun pick(y: Float) {
                    if (size.height <= 0) return
                    val i = ((y / size.height) * keys.size).toInt().coerceIn(0, keys.lastIndex)
                    val key = keys[i]
                    if (key != lastKey) {
                        lastKey = key
                        onScrub(key)
                    }
                }
                pick(down.position.y)
                down.consume()
                while (true) {
                    val event = awaitPointerEvent()
                    val change = event.changes.firstOrNull { it.id == down.id } ?: break
                    if (!change.pressed) break
                    pick(change.position.y)
                    change.consume()
                }
            }
        },
        verticalArrangement = Arrangement.SpaceEvenly,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        keys.forEach { Text(it, style = DenebType.meta, color = denebHint()) }
    }
}

private const val HANGUL_INITIALS = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ"
private const val HANGUL_ORDER = "ㄱㄴㄷㄹㅁㅂㅅㅇㅈㅊㅋㅌㅍㅎ"

/** The compact-index initial consonant of a Hangul syllable, or null if [c] isn't
 *  one. Double consonants fold to their base (ㄲ→ㄱ …) so the scrub stays 14 letters. */
private fun hangulInitial(c: Char): Char? {
    if (c.code < 0xAC00 || c.code > 0xD7A3) return null
    return when (val raw = HANGUL_INITIALS[(c.code - 0xAC00) / 588]) {
        'ㄲ' -> 'ㄱ'
        'ㄸ' -> 'ㄷ'
        'ㅃ' -> 'ㅂ'
        'ㅆ' -> 'ㅅ'
        'ㅉ' -> 'ㅈ'
        else -> raw
    }
}

/** Section key for a label: Hangul initial (syllable or standalone basic jamo), else
 *  uppercase Latin, else # — so CJK/accented/symbol labels share one bucket whose key
 *  matches their #-rank (otherwise each formed an orphan section colliding at rank 1000). */
private fun initialKey(label: String): String {
    val c = label.trimStart().firstOrNull() ?: return "#"
    hangulInitial(c)?.let { return it.toString() }
    if (c in HANGUL_ORDER) return c.toString() // standalone basic jamo → merge with its section
    return if (c in 'A'..'Z' || c in 'a'..'z') c.uppercaseChar().toString() else "#"
}

/** Sort order: Hangul (ㄱ→ㅎ), then Latin (A→Z), then # (digits/symbols) last. */
private fun sectionRank(key: String): Int {
    val c = key[0]
    val h = HANGUL_ORDER.indexOf(c)
    if (h >= 0) return h
    if (c in 'A'..'Z') return 100 + (c - 'A')
    return 1000
}

private fun sectionsOf(apps: List<LauncherAppEntry>): List<AppSection> = apps.groupBy { initialKey(it.label) }
    .toList()
    .sortedBy { (key, _) -> sectionRank(key) }
    .map { (key, list) -> AppSection(key, list.sortedBy { it.label.lowercase() }) }
