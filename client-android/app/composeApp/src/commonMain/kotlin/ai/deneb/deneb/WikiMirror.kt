package ai.deneb.deneb

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json

/**
 * On-device mirror of the whole wiki corpus (~550 pages / ~2.6MB) so 위키
 * browse·read·search keep working with no gateway — airplane mode, VPN down,
 * dead zone. Bulk-seeded from `miniapp.memory.mirror`, then kept current by
 * `wiki.changed` sync events (targeted page refetch/remove). Read paths fall
 * back here only when the network fetch fails, so an online session never
 * sees mirror staleness.
 *
 * Storage: [WikiMirrorFiles] (per-platform app-files dir), pages sharded into
 * [SHARD_COUNT] JSON maps so one page write rewrites ~1/16 of the corpus, not
 * all of it (dreamer bursts touch many pages in a row). meta.json carries the
 * owner fingerprint (url#token, like the section caches) so another gateway's
 * corpus is wiped instead of served, plus the last full-sync stamp.
 */
internal class WikiMirrorStore(
    private val files: WikiMirrorFiles,
    private val owner: () -> String,
) {
    private val mutex = Mutex()
    private var loaded = false
    private var pages = mutableMapOf<String, WikiPage>()
    private var meta = WikiMirrorMeta()

    suspend fun pageCount(): Int = mutex.withLock {
        loadLocked()
        pages.size
    }

    suspend fun syncedAtMs(): Long = mutex.withLock {
        loadLocked()
        meta.syncedAtMs
    }

    suspend fun get(path: String): WikiPage? = mutex.withLock {
        loadLocked()
        pages[path]
    }

    /** Pages filed under [category] (leading path directory; "" = root),
     *  newest-updated first — mirrors the list_in_category contract. */
    suspend fun listCategory(category: String): List<WikiPageRef> = mutex.withLock {
        loadLocked()
        pages.values
            .filter { wikiMirrorCategoryOf(it.path) == category }
            .map { WikiPageRef(it.path, it.title.ifBlank { it.path }, it.summary, it.updated) }
            .sortedWith(compareByDescending<WikiPageRef> { it.updated }.thenBy { it.title })
    }

    /** Category rollup for the 카테고리 browser; null when the mirror is empty
     *  so a caller can distinguish "no mirror yet" from "empty wiki". */
    suspend fun categories(): WikiCategories? = mutex.withLock {
        loadLocked()
        if (pages.isEmpty()) return@withLock null
        val counts = pages.values.groupingBy { wikiMirrorCategoryOf(it.path).ifBlank { "(root)" } }.eachCount()
        WikiCategories(
            categories = counts.entries
                .map { WikiCategory(it.key, it.value) }
                .sortedWith(compareByDescending<WikiCategory> { it.pageCount }.thenBy { it.name }),
            totalPages = pages.size,
            totalBytes = pages.values.sumOf { it.body.length.toLong() },
        )
    }

    /** Offline full-text search: every whitespace token must match (title,
     *  summary, tags, or body, case-insensitive); title/summary hits rank
     *  above body-only hits. Substring matching — fine for Korean. */
    suspend fun search(query: String, limit: Int = 20): List<SearchHit> = mutex.withLock {
        loadLocked()
        val tokens = query.trim().split(Regex("\\s+")).filter { it.isNotBlank() }
        if (tokens.isEmpty()) return@withLock emptyList()
        pages.values
            .mapNotNull { page ->
                var score = 0
                for (t in tokens) {
                    val inTitle = page.title.contains(t, ignoreCase = true)
                    val inHead = inTitle || page.summary.contains(t, ignoreCase = true) ||
                        page.tags.any { tag -> tag.contains(t, ignoreCase = true) }
                    val inBody = page.body.contains(t, ignoreCase = true)
                    if (!inHead && !inBody) return@mapNotNull null
                    score += if (inTitle) {
                        4
                    } else if (inHead) {
                        2
                    } else {
                        1
                    }
                }
                score to page
            }
            .sortedWith(compareByDescending<Pair<Int, WikiPage>> { it.first }.thenByDescending { it.second.updated })
            .take(limit)
            .map { (_, page) ->
                SearchHit(
                    path = page.path,
                    title = page.title.ifBlank { page.path },
                    snippet = wikiMirrorSnippet(page, tokens.first()),
                    category = wikiMirrorCategoryOf(page.path),
                )
            }
    }

    /** Atomically replace the whole mirror after a successful bulk pull. */
    suspend fun replaceAll(all: List<WikiPage>, nowMs: Long): Unit = mutex.withLock {
        loadLocked()
        pages = all.filter { it.path.isNotBlank() }.associateByTo(mutableMapOf()) { it.path }
        meta = WikiMirrorMeta(owner = owner(), syncedAtMs = nowMs)
        persistMetaLocked()
        for (shard in 0 until SHARD_COUNT) persistShardLocked(shard)
    }

    suspend fun upsert(page: WikiPage): Unit = mutex.withLock {
        loadLocked()
        if (page.path.isBlank()) return@withLock
        pages[page.path] = page
        persistShardLocked(shardOf(page.path))
        // First write claims ownership so a page-level update before any full
        // pull isn't wiped as foreign on the next load.
        if (meta.owner != owner()) {
            meta = meta.copy(owner = owner())
            persistMetaLocked()
        }
    }

    suspend fun remove(path: String): Unit = mutex.withLock {
        loadLocked()
        if (pages.remove(path) != null) persistShardLocked(shardOf(path))
    }

    /** Drop hot pages immediately on credential switch (non-suspend). Disk wipe
     *  follows via [clear]; reads are owner-guarded in [loadLocked] meanwhile. */
    fun evictMemoryForCredentialSwitch() {
        pages = mutableMapOf()
        meta = WikiMirrorMeta()
        loaded = true
    }

    /** Credential switch: drop memory and disk so account B never sees A's wiki. */
    suspend fun clear(): Unit = mutex.withLock {
        evictMemoryForCredentialSwitch()
        clearDiskLocked()
    }

    private fun clearDiskLocked() {
        runCatching {
            files.delete(META_FILE)
            for (shard in 0 until SHARD_COUNT) files.delete(shardFile(shard))
        }
    }

    private fun loadLocked() {
        if (loaded) {
            // Credentials can switch while pages stay hot in memory (clear() is async).
            // Never serve account A's corpus under account B's owner fingerprint.
            if (meta.owner.isNotEmpty() && meta.owner != owner()) {
                pages = mutableMapOf()
                meta = WikiMirrorMeta()
            }
            return
        }
        loaded = true
        val rawMeta = runCatching { files.read(META_FILE) }.getOrNull()
        val storedMeta = rawMeta?.let { runCatching { mirrorJson.decodeFromString(WikiMirrorMeta.serializer(), it) }.getOrNull() }
        if (storedMeta == null || storedMeta.owner != owner()) {
            // Foreign/corrupt mirror: wipe rather than serve another account's corpus.
            if (rawMeta != null || storedMeta != null) {
                runCatching {
                    files.delete(META_FILE)
                    for (shard in 0 until SHARD_COUNT) files.delete(shardFile(shard))
                }
            }
            return
        }
        meta = storedMeta
        for (shard in 0 until SHARD_COUNT) {
            val raw = runCatching { files.read(shardFile(shard)) }.getOrNull() ?: continue
            runCatching { mirrorJson.decodeFromString(shardSerializer, raw) }.getOrNull()?.let { pages.putAll(it) }
        }
    }

    private fun persistMetaLocked() {
        runCatching { files.write(META_FILE, mirrorJson.encodeToString(WikiMirrorMeta.serializer(), meta)) }
    }

    private fun persistShardLocked(shard: Int) {
        val slice = pages.filterKeys { shardOf(it) == shard }
        runCatching {
            if (slice.isEmpty()) {
                files.delete(shardFile(shard))
            } else {
                files.write(shardFile(shard), mirrorJson.encodeToString(shardSerializer, slice))
            }
        }
    }

    private companion object {
        const val SHARD_COUNT = 16
        const val META_FILE = "meta.json"

        val mirrorJson = Json { ignoreUnknownKeys = true }
        val shardSerializer = MapSerializer(String.serializer(), WikiPage.serializer())

        fun shardOf(path: String): Int = (path.hashCode() and 0x7fffffff) % SHARD_COUNT

        fun shardFile(shard: Int): String = "shard-$shard.json"
    }
}

@Serializable
internal data class WikiMirrorMeta(
    val owner: String = "",
    val syncedAtMs: Long = 0,
)

/** The category a wiki path files under (its leading directory). */
internal fun wikiMirrorCategoryOf(path: String): String = path.substringBeforeLast('/', missingDelimiterValue = "")

/** ~80 chars of body context around the first hit of [token] (else the head). */
internal fun wikiMirrorSnippet(page: WikiPage, token: String): String {
    val source = page.summary.ifBlank { page.body }
    val idx = source.indexOf(token, ignoreCase = true)
    val start = if (idx < 0) 0 else (idx - 24).coerceAtLeast(0)
    return source.drop(start).take(96).replace('\n', ' ').trim()
}

/**
 * Minimal string-file storage for the mirror, rooted at a per-platform
 * app-files subdirectory. All operations are best-effort — callers wrap in
 * runCatching; a miss just means the mirror reseeds.
 */
internal interface WikiMirrorFiles {
    fun read(name: String): String?

    fun write(name: String, content: String)

    fun delete(name: String)
}

/** Platform mirror storage under `<appFiles>/wiki_mirror/`. */
internal expect fun platformWikiMirrorFiles(): WikiMirrorFiles
