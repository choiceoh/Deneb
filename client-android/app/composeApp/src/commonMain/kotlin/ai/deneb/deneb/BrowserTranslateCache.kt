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
 * Bounded by a total character budget (keys + values) with least-recently-USED
 * eviction. Optionally disk-backed via [attachPersistence]: the newest entries
 * (within a smaller persist budget) are snapshotted to the cached-section store
 * on a character-count throttle, and seed the cache on the next app start — so
 * "recently visited pages" survive a restart. Best-effort: a lost tail or a
 * failed load just means those segments re-translate.
 */
internal class BrowserTranslateCache(
    private val maxChars: Int = DEFAULT_MAX_CHARS,
    private val persistThresholdChars: Int = DEFAULT_PERSIST_THRESHOLD_CHARS,
    private val persistBudgetChars: Int = DEFAULT_PERSIST_BUDGET_CHARS,
) {
    private val lock = SynchronousLock()
    private val entries = LinkedHashMap<String, String>()
    private var chars = 0
    private var persistSave: ((Map<String, String>) -> Unit)? = null
    private var unsavedChars = 0

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
        val toSave: Pair<(Map<String, String>) -> Unit, Map<String, String>>? = lock.withLock {
            bySegment.forEach { (segment, result) -> put(key(targetLang, segment), result) }
            unsavedChars += bySegment.entries.sumOf { it.key.length + it.value.length }
            val save = persistSave
            if (save != null && unsavedChars >= persistThresholdChars) {
                unsavedChars = 0
                save to newestEntriesWithinPersistBudget()
            } else {
                null
            }
        }
        // Disk write happens outside the lock (AppSettings I/O must not block lookups).
        toSave?.let { (save, snapshot) -> runCatching { save(snapshot) } }
        return segments.mapIndexed { i, segment -> cached[i] ?: bySegment.getValue(segment) }
    }

    /**
     * Wire disk persistence: [load] seeds the cache once (only into an empty
     * cache — live entries always win), [save] receives bounded newest-first
     * snapshots on a throttle. Idempotent; later attach calls are ignored.
     */
    fun attachPersistence(load: () -> Map<String, String>?, save: (Map<String, String>) -> Unit) {
        // null = already attached (idempotent no-op); false = attached but the
        // cache already holds live entries, so skip seeding.
        val shouldSeed: Boolean? = lock.withLock {
            if (persistSave != null) {
                null
            } else {
                persistSave = save
                entries.isEmpty()
            }
        }
        if (shouldSeed != true) return
        val seed = runCatching(load).getOrNull() ?: return
        lock.withLock {
            if (entries.isEmpty()) seed.forEach { (k, v) -> put(k, v) }
        }
    }

    /** Newest entries whose summed cost fits the persist budget, oldest-first
     *  so re-seeding preserves LRU order. Caller holds [lock]. */
    private fun newestEntriesWithinPersistBudget(): Map<String, String> {
        val newestFirst = entries.entries.toList().asReversed()
        var budget = persistBudgetChars
        val picked = ArrayList<Map.Entry<String, String>>()
        for (entry in newestFirst) {
            val cost = entry.key.length + entry.value.length
            if (cost > budget) break
            budget -= cost
            picked.add(entry)
        }
        return LinkedHashMap<String, String>(picked.size).apply {
            picked.asReversed().forEach { put(it.key, it.value) }
        }
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

        // Persist snapshots stay well under SectionDiskSlot's 256k-char envelope
        // cap (JSON escaping inflates Korean text), and the write throttle keeps
        // settings I/O off the per-batch hot path.
        const val DEFAULT_PERSIST_BUDGET_CHARS = 150_000
        const val DEFAULT_PERSIST_THRESHOLD_CHARS = 32_000
    }
}

/** App-lifetime cache instance shared across browser screen enter/leave. */
internal val browserTranslateCache = BrowserTranslateCache()
