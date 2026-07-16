package ai.deneb.deneb

import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.DashboardOut
import ai.deneb.deneb.generated.NotebookSummaryOut
import ai.deneb.deneb.generated.OrgTreeOut
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlin.time.TimeSource

/**
 * Session-scoped TTL cache for a section screen's one-shot fetch. The 더보기
 * section screens keep their state composition-local, so every entry re-runs the
 * fetch over the WAN: caching in the client fetch function makes a re-entry
 * within [ttl] instant (no network, no spinner) with zero screen changes, while
 * pull-to-refresh passes force=true and mutations invalidate. Same idea as the
 * calendar range cache and the approvals list cache, factored once.
 */
internal class SessionCache<T : Any>(private val ttl: Duration) {
    private var at: TimeSource.Monotonic.ValueTimeMark? = null
    private var value: T? = null

    /** The cached value while it is younger than the TTL, else null. */
    fun fresh(): T? = value?.takeIf { at?.let { mark -> mark.elapsedNow() < ttl } == true }

    fun store(v: T) {
        value = v
        at = TimeSource.Monotonic.markNow()
    }

    fun invalidate() {
        value = null
        at = null
    }
}

/** Shared staleness bound for the section caches — matches the calendar range
 *  cache scale. */
internal val SectionCacheTtl = 120.seconds

/**
 * Keyed [SessionCache] variant for per-argument fetches (category → page list,
 * path → page body). Bounded FIFO-evicted so page bodies can't grow without
 * limit across a long session.
 */
internal class SessionCacheMap<K : Any, V : Any>(
    private val ttl: Duration,
    private val maxEntries: Int = 32,
) {
    private val entries = LinkedHashMap<K, Pair<TimeSource.Monotonic.ValueTimeMark, V>>()

    fun fresh(key: K): V? = entries[key]?.takeIf { it.first.elapsedNow() < ttl }?.second

    fun store(key: K, value: V) {
        entries.remove(key)
        entries[key] = TimeSource.Monotonic.markNow() to value
        while (entries.size > maxEntries) entries.remove(entries.keys.first())
    }

    fun invalidate(key: K) {
        entries.remove(key)
    }
}

/**
 * The section caches, one per browse surface (카테고리·일기·사람·연락처·현황·조직도·
 * 노트북). Lives ON the client instance (not file-level) so a fresh client — a
 * test harness, a credential switch — starts cold instead of inheriting another
 * session's data.
 */
internal class SectionCaches {
    val categories = SessionCache<WikiCategories>(SectionCacheTtl)
    val diary = SessionCache<List<DiaryEntry>>(SectionCacheTtl)
    val people = SessionCache<List<PersonHit>>(SectionCacheTtl)
    val contacts = SessionCache<List<ContactRow>>(SectionCacheTtl)
    val dashboard = SessionCache<DashboardOut>(SectionCacheTtl)
    val org = SessionCache<OrgTreeOut>(SectionCacheTtl)
    val notebooks = SessionCache<List<NotebookSummaryOut>>(SectionCacheTtl)

    // Wiki browse loop: category → page list → page body. Wiki writes (save/
    // create/delete/move) invalidate the touched keys in DenebClientMemory.kt.
    val categoryPages = SessionCacheMap<String, List<WikiPageRef>>(SectionCacheTtl)
    val wikiPages = SessionCacheMap<String, WikiPage>(SectionCacheTtl, maxEntries = 24)
}
