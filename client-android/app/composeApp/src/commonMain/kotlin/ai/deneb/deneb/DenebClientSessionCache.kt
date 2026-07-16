package ai.deneb.deneb

import ai.deneb.data.AppSettings
import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.DashboardOut
import ai.deneb.deneb.generated.NotebookSummaryOut
import ai.deneb.deneb.generated.OrgTreeOut
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlin.time.TimeSource

/**
 * Session-scoped TTL cache for a section screen's one-shot fetch, optionally
 * disk-backed (cache-then-network, like the mail/feed/approvals caches): the
 * 더보기 section screens keep their state composition-local, so every entry
 * re-ran the fetch over the WAN. Caching in the client fetch function makes a
 * re-entry within [ttl] instant with zero screen changes; [peek] additionally
 * serves the last-known snapshot — memory or disk — for an instant stale paint
 * on cold start while the network refresh runs. Pull-to-refresh passes
 * force=true and mutations invalidate.
 */
internal class SessionCache<T : Any>(
    private val ttl: Duration,
    private val disk: SectionDiskSlot<T>? = null,
) {
    private var at: TimeSource.Monotonic.ValueTimeMark? = null
    private var value: T? = null
    private var diskChecked = false
    private var invalidationVersion = 0L
    private val loadMutex = Mutex()

    /** The cached value while it is younger than the TTL, else null. A disk-seeded
     *  value is never fresh — it paints, but the fetch still goes out. */
    private fun fresh(): T? = value?.takeIf { at?.let { mark -> mark.elapsedNow() < ttl } == true }

    /**
     * Reuse the fresh value or run one loader at a time. The second cache check
     * after acquiring the mutex collapses concurrent non-forced entries onto the
     * first request. A failed/cancelled loader leaves the cache untouched, and an
     * invalidation during loading prevents that now-stale result from being cached.
     */
    suspend fun getOrLoad(force: Boolean = false, load: suspend () -> T?): T? {
        if (!force) fresh()?.let { return it }
        return loadMutex.withLock {
            if (!force) fresh()?.let { return@withLock it }
            val version = invalidationVersion
            val loaded = load() ?: return@withLock null
            if (version == invalidationVersion) store(loaded)
            loaded
        }
    }

    /** Last-known value regardless of age — memory first, then one lazy disk read.
     *  For the screens' instant stale paint on cold start. */
    fun peek(): T? {
        value?.let { return it }
        if (!diskChecked) {
            diskChecked = true
            value = disk?.load()
        }
        return value
    }

    private fun store(v: T) {
        value = v
        at = TimeSource.Monotonic.markNow()
        diskChecked = true
        disk?.save(v)
    }

    fun invalidate() {
        invalidationVersion++
        value = null
        at = null
        diskChecked = true
        disk?.clear()
    }
}

/** Shared staleness bound for the section caches — matches the calendar range
 *  cache scale. */
internal val SectionCacheTtl = 120.seconds

/** Per-key mutexes with waiter accounting, removed as soon as the last user
 *  leaves. Same-key loads coalesce without serializing unrelated keys or
 *  retaining every key visited during a long session. */
private class KeyedMutex<K : Any> {
    private class Entry {
        val mutex = Mutex()
        var users = 0
    }

    private val guard = Mutex()
    private val entries = mutableMapOf<K, Entry>()

    suspend fun <T> withLock(key: K, action: suspend () -> T): T {
        val entry = guard.withLock {
            entries.getOrPut(key) { Entry() }.also { it.users++ }
        }
        try {
            return entry.mutex.withLock { action() }
        } finally {
            guard.withLock {
                entry.users--
                if (entry.users == 0 && entries[key] === entry) entries.remove(key)
            }
        }
    }
}

/**
 * Keyed [SessionCache] variant for per-argument fetches (category → page list,
 * path → page body, month range → events). Bounded by least-recently-stored
 * eviction so page bodies can't grow without limit across a long session. With
 * [disk] set, the most recent [diskMaxEntries] entries persist across restarts
 * and seed lazily as stale (paintable via [peek], never fresh).
 */
internal class SessionCacheMap<K : Any, V : Any>(
    private val ttl: Duration,
    private val maxEntries: Int = 32,
    private val disk: SectionDiskSlot<Map<K, V>>? = null,
    private val diskMaxEntries: Int = 8,
) {
    // null mark = disk-seeded (stale by definition).
    private val entries = LinkedHashMap<K, Pair<TimeSource.Monotonic.ValueTimeMark?, V>>()
    private var diskChecked = false
    private var invalidationVersion = 0L
    private val loadMutex = KeyedMutex<K>()

    private fun seedFromDisk() {
        if (diskChecked) return
        diskChecked = true
        disk?.load()?.forEach { (k, v) -> if (k !in entries) entries[k] = null to v }
    }

    private fun fresh(key: K): V? {
        seedFromDisk()
        val (mark, v) = entries[key] ?: return null
        return v.takeIf { mark != null && mark.elapsedNow() < ttl }
    }

    /** Reuse a fresh value or run one loader for [key]. Other keys remain
     *  independent, and invalidation while loading prevents stale recaching. */
    suspend fun getOrLoad(key: K, force: Boolean = false, load: suspend () -> V?): V? {
        if (!force) fresh(key)?.let { return it }
        return loadMutex.withLock(key) locked@{
            if (!force) fresh(key)?.let { return@locked it }
            val version = invalidationVersion
            val loaded = load() ?: return@locked null
            if (version == invalidationVersion) store(key, loaded)
            loaded
        }
    }

    /** Last-known value for [key] regardless of age — the cold-start stale paint. */
    fun peek(key: K): V? {
        seedFromDisk()
        return entries[key]?.second
    }

    private fun store(key: K, value: V) {
        seedFromDisk()
        entries.remove(key)
        entries[key] = TimeSource.Monotonic.markNow() to value
        while (entries.size > maxEntries) entries.remove(entries.keys.first())
        persist()
    }

    fun invalidate(key: K) {
        invalidationVersion++
        seedFromDisk()
        if (entries.remove(key) != null) persist()
    }

    /** Full reset (credential switch): drop memory and the disk snapshot. */
    fun clear() {
        invalidationVersion++
        entries.clear()
        diskChecked = true
        disk?.clear()
    }

    private fun persist() {
        val slot = disk ?: return
        // LinkedHashMap iteration is insertion order and store() reinserts, so the
        // tail is the most recently written — keep those.
        val recent = entries.entries.toList().takeLast(diskMaxEntries).associate { it.key to it.value.second }
        slot.save(recent)
    }
}

/**
 * One owner-fingerprinted JSON envelope in settings (encrypted at rest), the
 * disk backing of a [SessionCache]/[SessionCacheMap] slot. The owner check
 * (url#token fingerprint, [mailCacheOwner]) makes a prior gateway/account's
 * snapshot decode to null instead of rendering under new credentials; a
 * credential switch also purges via AppSettings.clearCachedContent (prefix).
 * All I/O is best-effort: corrupt, foreign-owner, and oversized entries miss
 * and are removed so they are not reparsed or rendered on later restarts.
 */
internal class SectionDiskSlot<T : Any>(
    private val appSettings: AppSettings,
    private val key: String,
    private val serializer: KSerializer<T>,
    private val owner: () -> String,
) {
    fun load(): T? {
        val raw = appSettings.getCachedSection(key) ?: return null
        val decoded = decodeOwnedCache(raw, owner(), "value", serializer)
        if (decoded == null) clear()
        return decoded
    }

    fun save(value: T) {
        runCatching {
            val json = encodeOwnedCache(owner(), "value", serializer, value)
            if (json.length <= MAX_SECTION_CACHE_CHARS) {
                appSettings.putCachedSection(key, json)
            } else {
                appSettings.removeCachedSection(key)
            }
        }
    }

    fun clear() {
        runCatching { appSettings.removeCachedSection(key) }
    }
}

// Backstop against one pathological snapshot bloating the settings store.
private const val MAX_SECTION_CACHE_CHARS = 256 * 1024

/**
 * The section caches, one per browse surface (카테고리·일기·사람·연락처·현황·조직도·
 * 노트북 + 위키 목록/본문 + 달력 월그리드). Lives ON the client instance so a fresh
 * client (test harness) starts cold; disk snapshots are owner-fingerprinted so a
 * credential switch can't leak the previous account's data.
 */
internal class SectionCaches(
    private val appSettings: AppSettings,
    private val owner: () -> String,
) {
    private fun <T : Any> slot(key: String, serializer: KSerializer<T>) = SectionDiskSlot(appSettings, key, serializer, owner)
    private fun <T : Any> cache(key: String, serializer: KSerializer<T>): SessionCache<T> = SessionCache(SectionCacheTtl, slot(key, serializer))

    private fun <V : Any> diskMapCache(
        key: String,
        valueSerializer: KSerializer<V>,
        diskMaxEntries: Int = 8,
    ): SessionCacheMap<String, V> = SessionCacheMap(
        SectionCacheTtl,
        disk = slot(key, MapSerializer(String.serializer(), valueSerializer)),
        diskMaxEntries = diskMaxEntries,
    )

    val categories = cache("categories", WikiCategories.serializer())
    val diary = cache("diary", ListSerializer(DiaryEntry.serializer()))
    val people = cache("people", ListSerializer(PersonHit.serializer()))
    val contacts = cache("contacts", ListSerializer(ContactRow.serializer()))
    val dashboard = cache("dashboard", DashboardOut.serializer())
    val org = cache("org", OrgTreeOut.serializer())
    val notebooks = cache("notebooks", ListSerializer(NotebookSummaryOut.serializer()))

    // Wiki browse loop: category → page list → page body. Wiki writes (save/
    // create/delete/move) invalidate the touched keys in DenebClientMemory.kt.
    // Page bodies stay memory-only (large, and staleness matters when editing).
    val categoryPages = diskMapCache(
        "category_pages",
        ListSerializer(WikiPageRef.serializer()),
    )
    val wikiPages = SessionCacheMap<String, WikiPage>(SectionCacheTtl, maxEntries = 24)

    // Calendar month-grid ranges (range-key → events): the last few viewed months
    // persist, so the grid paints dots instantly on cold start while the month
    // fetch runs. Replaces the old in-memory-only calRangeCache.
    val calendarRanges = diskMapCache(
        "calendar_ranges",
        ListSerializer(CalendarEvent.serializer()),
        diskMaxEntries = 4,
    )

    /** Full reset (credential switch): drop every memory value and disk snapshot. */
    fun clearAll() {
        categories.invalidate()
        diary.invalidate()
        people.invalidate()
        contacts.invalidate()
        dashboard.invalidate()
        org.invalidate()
        notebooks.invalidate()
        categoryPages.clear()
        wikiPages.clear()
        calendarRanges.clear()
    }
}
