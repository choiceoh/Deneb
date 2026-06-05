package com.inspiredandroid.kai.ui.markdown

// Parsing markdown is pure but not free — a long analysis is several ms. A LazyColumn
// disposes off-screen items, so the per-item `remember(content) { parseMarkdown }` cache
// is thrown away whenever a message scrolls out of view, and the body is re-parsed when
// it scrolls back in. That re-parse lands on the scroll frame and janks pure scrolling
// through a rich chat. This module-level LRU survives item disposal: each unique message
// body is parsed once and reused, and the LazyColumn's prefetch warms the next item's
// parse just ahead of the viewport so the work is off the visible frame.
//
// Composition (including prefetch) runs on the UI thread, so this is single-threaded — no
// locking, in keeping with the project's prefer-sequential style. Only finished
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
