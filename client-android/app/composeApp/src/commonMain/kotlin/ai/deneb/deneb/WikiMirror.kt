package ai.deneb.deneb

import ai.deneb.data.SynchronousLock
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
 * all of it (dreamer bursts touch many pages in a row). Each shard has two
 * slots; meta.json atomically selects one complete set. A failed write only
 * dirties inactive slots, while the prior manifest remains readable. The
 * manifest also carries the owner fingerprint (url#token, like the section
 * caches) and the last full-sync stamp.
 */
internal class WikiMirrorStore(
    private val files: WikiMirrorFiles,
    private val owner: () -> String,
) {
    private val mutex = Mutex()
    private val stateLock = SynchronousLock()
    private var loaded = false
    private var pages = mutableMapOf<String, WikiPage>()
    private var meta = WikiMirrorMeta()

    suspend fun pageCount(): Int = mutex.withLock {
        withStateLock {
            loadLocked()
            pages.size
        }
    }

    suspend fun syncedAtMs(): Long = mutex.withLock {
        withStateLock {
            loadLocked()
            meta.syncedAtMs
        }
    }

    suspend fun get(path: String): WikiPage? = mutex.withLock {
        withStateLock {
            loadLocked()
            pages[path]
        }
    }

    /** Pages filed under [category] (leading path directory; "" = root),
     *  newest-updated first — mirrors the list_in_category contract. */
    suspend fun listCategory(category: String): List<WikiPageRef> = mutex.withLock {
        withStateLock {
            loadLocked()
            pages.values
                .filter { wikiMirrorCategoryOf(it.path) == category }
                .map { WikiPageRef(it.path, it.title.ifBlank { it.path }, it.summary, it.updated) }
                .sortedWith(compareByDescending<WikiPageRef> { it.updated }.thenBy { it.title })
        }
    }

    /** Category rollup for the 카테고리 browser; null when the mirror is empty
     *  so a caller can distinguish "no mirror yet" from "empty wiki". */
    suspend fun categories(): WikiCategories? = mutex.withLock {
        withStateLock state@{
            loadLocked()
            if (pages.isEmpty()) return@state null
            val counts = pages.values.groupingBy { wikiMirrorCategoryOf(it.path).ifBlank { "(root)" } }.eachCount()
            // Korean aliases offline, mirroring the gateway's rule (alias == the
            // 대표 page's title) so a code-named project folder reads the same
            // whether or not the network is up.
            val aliases = pages.values
                .filter { it.path.endsWith("/대표.md") && it.title.isNotBlank() }
                .associate { it.path.removeSuffix("/대표.md") to it.title }
            WikiCategories(
                categories = counts.entries
                    .map { WikiCategory(it.key, it.value, aliasFor(it.key, aliases)) }
                    .sortedWith(compareByDescending<WikiCategory> { it.pageCount }.thenBy { it.name }),
                totalPages = pages.size,
                totalBytes = pages.values.sumOf { it.body.length.toLong() },
            )
        }
    }

    /** Offline full-text search: every whitespace token must match (title,
     *  summary, tags, or body, case-insensitive); title/summary hits rank
     *  above body-only hits. Substring matching — fine for Korean. */
    suspend fun search(query: String, limit: Int = 20): List<SearchHit> = mutex.withLock {
        withStateLock state@{
            loadLocked()
            val tokens = query.trim().split(Regex("\\s+")).filter { it.isNotBlank() }
            if (tokens.isEmpty()) return@state emptyList()
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
    }

    /** Atomically replace the whole mirror after a successful bulk pull.
     *  [expectedOwner] pins the account fingerprint captured when the pull
     *  started so a credential switch after the RPC fence cannot stamp
     *  account A's corpus with account B's owner. */
    suspend fun replaceAll(all: List<WikiPage>, nowMs: Long, expectedOwner: String? = null): Boolean = mutex.withLock {
        withStateLock state@{
            if (expectedOwner != null && owner() != expectedOwner) return@state false
            loadLocked()
            val nextPages = all.filter { it.path.isNotBlank() }.associateByTo(mutableMapOf()) { it.path }
            val nextMeta = commitLocked(
                nextPages = nextPages,
                nextOwner = expectedOwner ?: owner(),
                syncedAtMs = nowMs,
                changedShards = ALL_SHARDS,
            ) ?: return@state false
            pages = nextPages
            meta = nextMeta
            true
        }
    }

    suspend fun upsert(page: WikiPage, expectedOwner: String? = null): Boolean = mutex.withLock {
        withStateLock state@{
            if (expectedOwner != null && owner() != expectedOwner) return@state false
            loadLocked()
            if (page.path.isBlank()) return@state true
            val nextPages = pages.toMutableMap().apply { put(page.path, page) }
            val nextMeta = commitLocked(
                nextPages = nextPages,
                nextOwner = expectedOwner ?: owner(),
                syncedAtMs = meta.syncedAtMs,
                changedShards = setOf(shardOf(page.path)),
            ) ?: return@state false
            pages = nextPages
            meta = nextMeta
            true
        }
    }

    suspend fun remove(path: String, expectedOwner: String? = null): Boolean = mutex.withLock {
        withStateLock state@{
            if (expectedOwner != null && owner() != expectedOwner) return@state false
            loadLocked()
            if (path !in pages) return@state true
            val nextPages = pages.toMutableMap().apply { remove(path) }
            val nextMeta = commitLocked(
                nextPages = nextPages,
                nextOwner = expectedOwner ?: owner(),
                syncedAtMs = meta.syncedAtMs,
                changedShards = setOf(shardOf(path)),
            ) ?: return@state false
            pages = nextPages
            meta = nextMeta
            true
        }
    }

    /** Drop hot pages immediately on credential switch (non-suspend). Disk wipe
     *  follows via [clear]; reads are owner-guarded in [loadLocked] meanwhile. */
    fun evictMemoryForCredentialSwitch() = withStateLock {
        evictMemoryLocked()
    }

    private fun evictMemoryLocked() {
        pages = mutableMapOf()
        meta = WikiMirrorMeta()
        loaded = true
    }

    /** Credential switch: drop memory and disk so account B never sees A's wiki. */
    suspend fun clear(): Unit = mutex.withLock {
        withStateLock {
            evictMemoryLocked()
            clearDiskLocked()
        }
    }

    private fun <T> withStateLock(action: () -> T): T = stateLock.withLock(action)

    private fun clearDiskLocked() {
        runCatching { files.delete(META_FILE) }
        for (shard in 0 until SHARD_COUNT) {
            runCatching { files.delete(legacyShardFile(shard)) }
            for (slot in SHARD_SLOTS) {
                runCatching { files.delete(shardFile(shard, slot)) }
            }
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
        val rawMeta = runCatching { files.read(META_FILE) }.getOrNull()
        val storedMeta = rawMeta?.let { runCatching { mirrorJson.decodeFromString(WikiMirrorMeta.serializer(), it) }.getOrNull() }
        if (storedMeta == null || storedMeta.owner != owner()) {
            // Foreign/corrupt mirror: wipe rather than serve another account's corpus.
            if (rawMeta != null) clearDiskLocked()
            loaded = true
            return
        }

        val storedPages = when (storedMeta.storageVersion) {
            LEGACY_STORAGE_VERSION -> loadLegacyPagesLocked()
            TRANSACTIONAL_STORAGE_VERSION -> loadTransactionalPagesLocked(storedMeta)
            else -> null
        }
        if (storedPages == null) {
            clearDiskLocked()
            loaded = true
            return
        }
        pages = storedPages
        meta = storedMeta
        loaded = true
    }

    private fun loadLegacyPagesLocked(): MutableMap<String, WikiPage> {
        val loadedPages = mutableMapOf<String, WikiPage>()
        for (shard in 0 until SHARD_COUNT) {
            val raw = runCatching { files.read(legacyShardFile(shard)) }.getOrNull() ?: continue
            runCatching { mirrorJson.decodeFromString(shardSerializer, raw) }.getOrNull()?.let { loadedPages.putAll(it) }
        }
        return loadedPages
    }

    private fun loadTransactionalPagesLocked(storedMeta: WikiMirrorMeta): MutableMap<String, WikiPage>? {
        if (!storedMeta.hasValidShardSlots()) return null
        val loadedPages = mutableMapOf<String, WikiPage>()
        for ((shard, slot) in storedMeta.shardSlots.withIndex()) {
            if (slot == EMPTY_SHARD_SLOT) continue
            val raw = runCatching { files.read(shardFile(shard, slot)) }.getOrNull() ?: return null
            val slice = runCatching { mirrorJson.decodeFromString(shardSerializer, raw) }.getOrNull() ?: return null
            if (slice.keys.any { it.isBlank() || shardOf(it) != shard }) return null
            loadedPages.putAll(slice)
        }
        return loadedPages
    }

    /**
     * Write changed shards to inactive slots, then switch the manifest. The
     * manifest write is the only commit point; platform implementations replace
     * it atomically. Legacy data takes the full-shard path on its first mutation.
     */
    private fun commitLocked(
        nextPages: Map<String, WikiPage>,
        nextOwner: String,
        syncedAtMs: Long,
        changedShards: Set<Int>,
    ): WikiMirrorMeta? {
        val currentIsTransactional = meta.storageVersion == TRANSACTIONAL_STORAGE_VERSION && meta.hasValidShardSlots()
        val shardsToWrite = if (currentIsTransactional && meta.owner == nextOwner) changedShards else ALL_SHARDS
        val nextSlots = if (currentIsTransactional && meta.owner == nextOwner) {
            meta.shardSlots.toMutableList()
        } else {
            MutableList(SHARD_COUNT) { EMPTY_SHARD_SLOT }
        }

        val prepared = runCatching {
            for (shard in shardsToWrite) {
                val slice = nextPages.filterKeys { shardOf(it) == shard }
                if (slice.isEmpty()) {
                    nextSlots[shard] = EMPTY_SHARD_SLOT
                } else {
                    val slot = inactiveSlot(nextSlots[shard])
                    files.write(shardFile(shard, slot), mirrorJson.encodeToString(shardSerializer, slice))
                    nextSlots[shard] = slot
                }
            }
            if (owner() != nextOwner) return null
            WikiMirrorMeta(
                owner = nextOwner,
                syncedAtMs = syncedAtMs,
                storageVersion = TRANSACTIONAL_STORAGE_VERSION,
                revision = nextRevision(meta.revision),
                shardSlots = nextSlots,
            ).also { committed ->
                files.write(META_FILE, mirrorJson.encodeToString(WikiMirrorMeta.serializer(), committed))
            }
        }.getOrNull() ?: return null

        cleanupInactiveFilesLocked(prepared)
        return prepared
    }

    private fun cleanupInactiveFilesLocked(committed: WikiMirrorMeta) {
        for ((shard, activeSlot) in committed.shardSlots.withIndex()) {
            runCatching { files.delete(legacyShardFile(shard)) }
            for (slot in SHARD_SLOTS) {
                if (slot != activeSlot) runCatching { files.delete(shardFile(shard, slot)) }
            }
        }
    }

    private companion object {
        const val SHARD_COUNT = 16
        const val META_FILE = "meta.json"
        const val LEGACY_STORAGE_VERSION = 0
        const val TRANSACTIONAL_STORAGE_VERSION = 1
        const val EMPTY_SHARD_SLOT = -1
        val SHARD_SLOTS = 0..1
        val ALL_SHARDS = (0 until SHARD_COUNT).toSet()

        val mirrorJson = Json { ignoreUnknownKeys = true }
        val shardSerializer = MapSerializer(String.serializer(), WikiPage.serializer())

        fun shardOf(path: String): Int = (path.hashCode() and 0x7fffffff) % SHARD_COUNT

        fun legacyShardFile(shard: Int): String = "shard-$shard.json"

        fun shardFile(shard: Int, slot: Int): String = "shard-$shard-$slot.json"

        fun inactiveSlot(activeSlot: Int): Int = if (activeSlot == 0) 1 else 0

        fun nextRevision(current: Long): Long = if (current == Long.MAX_VALUE) 1 else current + 1
    }
}

@Serializable
internal data class WikiMirrorMeta(
    val owner: String = "",
    val syncedAtMs: Long = 0,
    val storageVersion: Int = 0,
    val revision: Long = 0,
    val shardSlots: List<Int> = emptyList(),
) {
    fun hasValidShardSlots(): Boolean = shardSlots.size == 16 && shardSlots.all { it in -1..1 }
}

/** The category a wiki path files under (its leading directory). */
internal fun wikiMirrorCategoryOf(path: String): String = path.substringBeforeLast('/', missingDelimiterValue = "")

/** Korean alias for a category path, from folder → 대표-title [aliases]. Matches
 *  the project folder itself ("프로젝트/<code>") and its slots
 *  ("프로젝트/<code>/메일분석"), which resolve to the same owning project. An
 *  alias equal to the folder name adds nothing, so it is dropped. */
internal fun aliasFor(category: String, aliases: Map<String, String>): String {
    var probe = category
    while (probe.isNotBlank()) {
        aliases[probe]?.let { if (it != probe.substringAfterLast('/')) return it }
        probe = probe.substringBeforeLast('/', missingDelimiterValue = "")
    }
    return ""
}

/** ~80 chars of body context around the first hit of [token] (else the head). */
internal fun wikiMirrorSnippet(page: WikiPage, token: String): String {
    val source = page.summary.ifBlank { page.body }
    val idx = source.indexOf(token, ignoreCase = true)
    val start = if (idx < 0) 0 else (idx - 24).coerceAtLeast(0)
    return source.drop(start).take(96).replace('\n', ' ').trim()
}

/**
 * Minimal string-file storage for the mirror, rooted at a per-platform
 * app-files subdirectory. [write] must atomically replace one complete file:
 * the manifest relies on that operation as its commit point.
 */
internal interface WikiMirrorFiles {
    fun read(name: String): String?

    fun write(name: String, content: String)

    fun delete(name: String)
}

/** Platform mirror storage under `<appFiles>/wiki_mirror/`. */
internal expect fun platformWikiMirrorFiles(): WikiMirrorFiles
