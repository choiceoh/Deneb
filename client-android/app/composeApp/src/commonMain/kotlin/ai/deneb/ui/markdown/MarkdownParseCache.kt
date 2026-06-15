package ai.deneb.ui.markdown

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.withContext

// Parsing markdown is pure but not free — a long analysis is several ms. A LazyColumn
// disposes off-screen items, so the per-item `remember(content) { parseMarkdown }` cache
// is thrown away whenever a message scrolls out of view, and the body is re-parsed when
// it scrolls back in. That re-parse lands on the scroll frame and janks pure scrolling
// through a rich chat. This module-level LRU survives item disposal: each unique message
// body is parsed once and reused, and the LazyColumn's prefetch warms the next item's
// parse just ahead of the viewport so the work is off the visible frame.
//
// Cache access is UI-thread only — no locking, in keeping with the project's
// prefer-sequential style. precomputeMarkdownAsync keeps that invariant while still using
// the device's spare cores: the heavy parse fans out across Dispatchers.Default, but every
// cache read/insert happens back on the UI thread (its caller). Only finished
// (non-streaming) bodies are cached; streaming intermediates would churn the LRU.
private const val MARKDOWN_PARSE_CACHE_MAX = 128

// Access-order LRU built on a plain Kotlin LinkedHashMap (the JVM accessOrder constructor
// and removeEldestEntry are not in the common stdlib): a hit re-inserts the entry to mark
// it most-recently-used, and eviction drops the first (least-recently-used) key.
private val markdownParseCache = LinkedHashMap<String, MarkdownDocument>()

fun parseMarkdownCached(text: String): MarkdownDocument {
    markdownParseCache.remove(text)?.let { cached ->
        markdownParseCache[text] = cached // re-insert at the tail = most recently used
        return cached
    }
    val doc = parseMarkdown(text)
    markdownParseCache[text] = doc
    while (markdownParseCache.size > MARKDOWN_PARSE_CACHE_MAX) {
        markdownParseCache.remove(markdownParseCache.keys.first())
    }
    return doc
}

// isMarkdownCached reports whether [text] is already parsed (plain lookup, no LRU touch,
// no parse). UI-thread only, like the rest of this cache.
fun isMarkdownCached(text: String): Boolean = markdownParseCache.containsKey(text)

// putMarkdownParsed inserts an already-parsed [doc] without parsing — used by
// precomputeMarkdownAsync to store a result computed on a background core. UI-thread only.
fun putMarkdownParsed(text: String, doc: MarkdownDocument) {
    if (markdownParseCache.containsKey(text)) return
    markdownParseCache[text] = doc
    while (markdownParseCache.size > MARKDOWN_PARSE_CACHE_MAX) {
        markdownParseCache.remove(markdownParseCache.keys.first())
    }
}

// precomputeMarkdownAsync warms the cache for [texts] using spare CPU cores: the pure parse
// (several ms each) fans out across Dispatchers.Default, while the cache reads and inserts
// stay on the CALLER's context. Call it from a UI-thread coroutine (a LaunchedEffect) so
// the single-threaded cache is never touched off the UI thread. By the time a message
// scrolls into view its body is already parsed, so composition's parseMarkdownCached is a
// hit instead of an on-frame parse.
suspend fun precomputeMarkdownAsync(texts: List<String>) {
    val pending = texts.asSequence()
        .filter { it.isNotBlank() && !isMarkdownCached(it) }
        .distinct()
        .toList()
    if (pending.isEmpty()) return
    // Parse in parallel on background cores; withContext returns to the caller (UI) context,
    // where the cheap inserts are serialized with composition's own cache reads.
    val parsed = withContext(Dispatchers.Default) {
        pending.map { text -> async { text to parseMarkdown(text) } }.awaitAll()
    }
    parsed.forEach { (text, doc) -> putMarkdownParsed(text, doc) }
}
