package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import ai.deneb.ui.text.koreanTextSections
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * A Korean-first alphabetical list with a ㄱㄴㄷ/A–Z scrub index — the shared shape
 * behind both the app drawer and the 전체 연락처 browser. Groups [items] by initial
 * ([koreanTextSections]), renders header + [row] per section in one LazyColumn, and
 * overlays a right-edge scrub strip: drag it to jump to a section (Niagara idiom).
 * Pure presentation; the caller supplies [label] (for sectioning), [key] (stable
 * item identity), and the [row] content. Monochrome, text-first — no icons.
 */
@Composable
fun <T> SectionedScrubList(
    items: List<T>,
    label: (T) -> String,
    key: (T) -> Any,
    modifier: Modifier = Modifier,
    row: @Composable (T) -> Unit,
) {
    val sections = remember(items) { koreanTextSections(items, label) }
    // Flat LazyColumn index of each section header, so the scrub can scrollToItem.
    val headerIndex = remember(sections) {
        buildMap {
            var idx = 0
            sections.forEach {
                put(it.key, idx)
                idx += 1 + it.items.size
            }
        }
    }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    Box(modifier.fillMaxSize()) {
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            // Leave room on the right for the scrub index.
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp, end = 24.dp),
        ) {
            sections.forEach { sec ->
                item(key = "h:${sec.key}") { SectionHeader(sec.key) }
                items(sec.items, key = { key(it) }) { item -> row(item) }
            }
        }
        // Scrub index: drag over the letters to jump to that section (Niagara idiom).
        // Hidden when there's only one section (nothing to scrub between).
        if (sections.size > 1) {
            ScrubIndex(
                keys = sections.map { it.key },
                onScrub = { k -> headerIndex[k]?.let { i -> scope.launch { listState.scrollToItem(i) } } },
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
