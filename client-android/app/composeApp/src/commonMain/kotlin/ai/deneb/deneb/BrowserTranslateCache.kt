package ai.deneb.deneb

import ai.deneb.data.SynchronousLock

/**
 * Segment-level LRU cache for the in-app browser's DeepL translations.
 *
 * The injected page translator re-collects and re-ships every text segment on
 * each page load, so revisiting a recent page (back-nav, reload, SPA soft-nav)
 * used to re-translate everything through `miniapp.web.translate`. Caching at
 * SEGMENT granularity — not per page — also hits shared chrome (menus, headers,
 * footers) across different pages of the same site.
 *
 * App-process lifetime, bounded by a total character budget (keys + values) with
 * least-recently-USED eviction. Not persisted: translations are cheap to redo
 * after an app restart and AppSettings storage should not grow with page text.
 */
internal class BrowserTranslateCache(
    private val maxChars: Int = DEFAULT_MAX_CHARS,
) {
    private val lock = SynchronousLock()
    private val entries = LinkedHashMap<String, String>()
    private var chars = 0

    /**
     * Translate [segments] to [targetLang], serving cached segments locally and
     * calling [translate] only for the misses (deduplicated). Returns a
     * same-length, same-order list, or null when misses exist and [translate]
     * fails/returns null — matching the bridge's drop-the-batch contract so the
     * page keeps its originals and can retry later.
     */
    suspend fun translate(
        segments: List<String>,
        targetLang: String,
        translate: suspend (List<String>, String) -> List<String>?,
    ): List<String>? {
        if (segments.isEmpty()) return emptyList()
        val cached = lock.withLock { segments.map { touch(key(targetLang, it)) } }
        val missSegments = LinkedHashSet<String>()
        segments.forEachIndexed { i, segment ->
            if (cached[i] == null) missSegments.add(segment)
        }
        if (missSegments.isEmpty()) return cached.map { it!! }

        val missList = missSegments.toList()
        val translated = translate(missList, targetLang) ?: return null
        if (translated.size != missList.size) return null

        val bySegment = missList.indices.associate { missList[it] to translated[it] }
        lock.withLock {
            bySegment.forEach { (segment, result) -> put(key(targetLang, segment), result) }
        }
        return segments.mapIndexed { i, segment -> cached[i] ?: bySegment.getValue(segment) }
    }

    /** Current number of cached segments (test/diagnostic). */
    fun size(): Int = lock.withLock { entries.size }

    private fun key(targetLang: String, segment: String): String = targetLang + " " + segment

    /** Lookup that refreshes LRU recency on hit. Caller holds [lock]. */
    private fun touch(key: String): String? {
        val value = entries.remove(key) ?: return null
        entries[key] = value
        return value
    }

    /** Insert + evict oldest until within budget. Caller holds [lock]. */
    private fun put(key: String, value: String) {
        val cost = key.length + value.length
        if (cost > maxChars) return // a pathological giant segment must not wipe the cache
        entries.remove(key)?.let { chars -= key.length + it.length }
        entries[key] = value
        chars += cost
        val iterator = entries.entries.iterator()
        while (chars > maxChars && iterator.hasNext()) {
            val oldest = iterator.next()
            chars -= oldest.key.length + oldest.value.length
            iterator.remove()
        }
    }

    companion object {
        // ~500k chars ≈ a handful of heavy article pages plus shared site chrome.
        const val DEFAULT_MAX_CHARS = 500_000
    }
}

/** App-lifetime cache instance shared across browser screen enter/leave. */
internal val browserTranslateCache = BrowserTranslateCache()
