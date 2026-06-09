package ai.deneb.ui.markdown

import androidx.compose.material3.ColorScheme
import androidx.compose.ui.text.AnnotatedString

// toAnnotatedString walks a block's inline nodes and applies spans. The per-item remember()
// that holds the result is dropped when a message scrolls out of view, so it rebuilds when the
// message scrolls back in. Because parseMarkdownCached reuses the same MarkdownDocument across
// scroll, the same inline-list instance recurs — so this cache keys by reference (===) and
// skips the rebuild, without the O(n) hashCode a content-keyed map would cost on every lookup.
// KMP common has no IdentityHashMap, so a small FIFO ring scanned by reference is used;
// reference compares are ~free. Single-threaded (Compose UI thread), in keeping with the
// project's prefer-sequential style.
private const val ANNOTATED_CACHE_MAX = 256

private class AnnotatedEntry(val nodes: Any, val colors: ColorScheme, val value: AnnotatedString)

private val annotatedCache = ArrayDeque<AnnotatedEntry>()

// `nodes` is the inline-list instance from the cached document; identity is the key. `build`
// only runs on a miss (a fresh parse, a theme change, or an evicted entry).
fun cachedAnnotatedString(nodes: Any, colors: ColorScheme, build: () -> AnnotatedString): AnnotatedString {
    for (entry in annotatedCache) {
        if (entry.nodes === nodes && entry.colors === colors) return entry.value
    }
    val built = build()
    annotatedCache.addLast(AnnotatedEntry(nodes, colors, built))
    while (annotatedCache.size > ANNOTATED_CACHE_MAX) annotatedCache.removeFirst()
    return built
}
