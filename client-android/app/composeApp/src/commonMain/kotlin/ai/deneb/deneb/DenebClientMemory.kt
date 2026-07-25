package ai.deneb.deneb

import ai.deneb.data.MemoryEntry
import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.SearchAllResult
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray

/**
 * Knowledge surface of [DenebGatewayClient]: wiki/memory pages and categories
 * (`miniapp.memory.*`), diary, unified search (`miniapp.search.all`), and people
 * (`miniapp.people.list`). Extensions so the gateway client stays one facade
 * while each RPC domain lives in its own file.
 */

/** Refresh the wiki-page snapshot behind the memory screen (titles only — see
 *  the read-only note on DenebGatewayClient's memory overrides). */
internal suspend fun DenebGatewayClient.refreshMemories() {
    val payload = callRpc<MemoryListPayload>(
        "miniapp.memory.list_in_category",
        buildJsonObject {
            put("category", "")
            put("limit", 200)
        },
    ) ?: return
    _denebMemories.value = payload.pages
        .filter { it.path.isNotBlank() }
        .distinctByLast { it.path }
        .map { p ->
            MemoryEntry(
                key = p.path,
                content = p.summary.ifBlank { p.title.ifBlank { p.path } },
                createdAt = 0,
                updatedAt = 0,
            )
        }
}

/** All wiki categories with page counts + corpus totals (`memory.categories`).
 *  Session-cached so re-entering the section within the TTL skips the network. */
suspend fun DenebGatewayClient.fetchCategories(force: Boolean = false): WikiCategories? {
    return sectionCaches.categories.getOrLoad(force) {
        val p = callRpc<CategoriesPayload>("miniapp.memory.categories", buildJsonObject {})
            ?: return@getOrLoad null
        WikiCategories(
            categories = p.categories.map { WikiCategory(it.name, it.pageCount) },
            totalPages = p.totalPages,
            totalBytes = p.totalBytes,
        )
        // Offline: the mirror's rollup, outside the session cache so a hit
        // can't mask the next online fetch (null with no mirror → retry UI).
    } ?: wikiMirror.categories()
}

/** Pages within one category (`memory.list_in_category`); blank lists all.
 *  Null on a fetch failure so the screen can offer retry instead of showing a
 *  misleading "empty category". Session-cached per category so browsing back
 *  and forth within the TTL skips the network; wiki writes invalidate. */
suspend fun DenebGatewayClient.fetchCategoryPages(category: String, force: Boolean = false): List<WikiPageRef>? {
    return sectionCaches.categoryPages.getOrLoad(category, force) {
        val p = callRpc<MemoryListPayload>(
            "miniapp.memory.list_in_category",
            buildJsonObject {
                put("category", category)
                put("limit", 200)
            },
        ) ?: return@getOrLoad null
        p.pages
            .filter { it.path.isNotBlank() }
            .distinctByLast { it.path }
            .map { WikiPageRef(it.path, it.title.ifBlank { it.path }, it.summary, it.updated) }
        // Offline: the mirror's listing, outside the session cache so a hit
        // can't mask the next online fetch (empty → null → retry UI).
    } ?: wikiMirror.listCategory(if (category == "(root)") "" else category).ifEmpty { null }
}

/** The category a wiki path files under (its leading directory), for targeted
 *  category-list invalidation on a write. */
private fun wikiCategoryOf(path: String): String = path.substringBeforeLast('/', missingDelimiterValue = "")

private const val DIARY_RECENT_DEFAULT_LIMIT = 30

/** Recent diary entries for the timeline (`miniapp.memory.diary_recent`).
 *  Null on a fetch failure so the screen can offer retry instead of showing
 *  a misleading empty timeline. The default-limit read is session-cached so
 *  re-entering the section within the TTL skips the network. */
suspend fun DenebGatewayClient.fetchRecentDiary(
    limit: Int = DIARY_RECENT_DEFAULT_LIMIT,
    force: Boolean = false,
): List<DiaryEntry>? {
    val cacheable = limit == DIARY_RECENT_DEFAULT_LIMIT
    suspend fun load(): List<DiaryEntry>? {
        val p = callRpc<DiaryRecentPayload>(
            "miniapp.memory.diary_recent",
            buildJsonObject { put("limit", limit) },
        ) ?: return null
        return p.entries.map { DiaryEntry(header = it.header, content = it.content, file = it.file) }
    }
    return if (cacheable) sectionCaches.diary.getOrLoad(force, ::load) else load()
}

/** Delete one or more wiki pages by path (`miniapp.memory.delete_pages`).
 *  The backend deletes best-effort and reports a per-page failure list, so
 *  this returns true only when every requested page was actually removed —
 *  letting the category screen surface a partial failure instead of
 *  silently dropping unselected rows. */
suspend fun DenebGatewayClient.deleteCategoryPages(paths: List<String>): Boolean {
    val uniquePaths = paths.distinct()
    if (uniquePaths.isEmpty()) return true
    sectionCaches.categories.invalidate()
    uniquePaths.forEach { path ->
        sectionCaches.wikiPages.invalidate(path)
        sectionCaches.categoryPages.invalidate(wikiCategoryOf(path))
    }
    val resp = callRpc<DeletePagesPayload>(
        "miniapp.memory.delete_pages",
        buildJsonObject {
            putJsonArray("paths") { uniquePaths.forEach { add(it) } }
        },
    ) ?: return false
    return resp.ok && resp.deleted == uniquePaths.size
}

/** Move one wiki page to a new path (`miniapp.memory.move_page`). The bucket is
 *  the path's leading directory, so this is how a page is reclassified. The
 *  backend rejects a traversal path, an invalid target category, or an existing
 *  target (no overwrite); returns true only when the move actually happened. */
suspend fun DenebGatewayClient.moveWikiPage(from: String, to: String): Boolean {
    sectionCaches.categories.invalidate()
    for (path in listOf(from, to)) {
        sectionCaches.wikiPages.invalidate(path)
        sectionCaches.categoryPages.invalidate(wikiCategoryOf(path))
    }
    val resp = callRpc<MovePagePayload>(
        "miniapp.memory.move_page",
        buildJsonObject {
            put("from", from)
            put("to", to)
        },
    ) ?: return false
    return resp.ok
}

/** Reclassify several pages into [targetCategory] — one move_page call each,
 *  filing each page under `<targetCategory>/<basename>`. Returns the count
 *  actually moved so the screen can report a partial move (e.g. a name already
 *  taken in the target). A page already in the target category is skipped. */
suspend fun DenebGatewayClient.moveCategoryPages(paths: List<String>, targetCategory: String): Int {
    var moved = 0
    for (p in paths.distinct()) {
        val base = p.substringAfterLast('/')
        val to = "$targetCategory/$base"
        if (p == to) continue
        if (moveWikiPage(p, to)) moved++
    }
    return moved
}

/** Full wiki/memory page by path (`miniapp.memory.get_page`). Session-cached
 *  per path (bounded) so re-opening a just-read page is instant; a save to the
 *  path invalidates, so the post-save refetch is always fresh. */
suspend fun DenebGatewayClient.fetchWikiPage(path: String, force: Boolean = false): WikiPage? = sectionCaches.wikiPages.getOrLoad(path, force) {
    callRpc<WikiPagePayload>(
        "miniapp.memory.get_page",
        buildJsonObject { put("path", path) },
    )?.toWikiPage(fallbackPath = path)
    // Offline: the mirror's copy (whole corpus on disk, WikiMirror.kt),
    // outside the session cache so the next online view refetches fresh.
} ?: wikiMirror.get(path)

/** Overwrite a wiki page; non-null title/summary/tags also update frontmatter. */
suspend fun DenebGatewayClient.saveWikiPage(
    path: String,
    body: String,
    title: String? = null,
    summary: String? = null,
    tags: List<String>? = null,
): Boolean {
    // The page body changed and its title/summary feed the category list rows.
    sectionCaches.wikiPages.invalidate(path)
    sectionCaches.categoryPages.invalidate(wikiCategoryOf(path))
    return callRpc<JsonObject>(
        "miniapp.memory.write_page",
        buildJsonObject {
            put("path", path)
            put("body", body)
            if (title != null) put("title", title)
            if (summary != null) put("summary", summary)
            if (tags != null) putJsonArray("tags") { tags.forEach { add(it) } }
        },
    ) != null
}

/** Create a new wiki page (`miniapp.memory.create_page`); returns its path. */
suspend fun DenebGatewayClient.createWikiPage(title: String, category: String, body: String): String? {
    sectionCaches.categories.invalidate()
    sectionCaches.categoryPages.invalidate(category)
    return callRpc<WikiPagePayload>(
        "miniapp.memory.create_page",
        buildJsonObject {
            put("title", title)
            put("category", category)
            put("body", body)
        },
    )?.path
        ?.takeIf { it.isNotBlank() }
}

/** Unified search across wiki, diary and people (`miniapp.search.all`).
 *  Offline, wiki and diary hits come from the on-device mirrors, and people
 *  hits are the 인물 wiki pages already inside the wiki mirror (the Gmail
 *  half of the people directory is inherently online-only); null only when
 *  there's no mirror content at all, so the screen retries. */
suspend fun DenebGatewayClient.searchAll(query: String): SearchResults? {
    val p = callRpc<SearchAllResult>(
        "miniapp.search.all",
        buildJsonObject {
            put("query", query)
            put("limit", 20)
        },
    ) ?: return offlineSearchAll(query)
    return SearchResults(
        wiki = p.wiki.filter { it.path.isNotBlank() }
            .map { SearchHit(it.path, it.title.ifBlank { it.path }, it.snippet.ifBlank { it.summary }, it.category) },
        diary = p.diary.map { SearchHit("", it.header.ifBlank { "일기" }, it.content, "diary") },
        people = p.people.filter { it.email.isNotBlank() || it.name.isNotBlank() }
            .map { PersonHit(it.name.ifBlank { it.email }, it.email, it.messageCount, it.lastSubject) },
    )
}

/** Offline 검색: wiki + diary from the on-device mirrors; people are the 인물
 *  mirror pages surfaced as wiki-only rows (blank email, zero mail count —
 *  the shape PersonHit documents for wiki-only people). */
internal suspend fun DenebGatewayClient.offlineSearchAll(query: String): SearchResults? {
    val wiki = wikiMirror.search(query)
    val diary = diaryMirror.search(query)
    if (wiki.isEmpty() && diary.isEmpty()) return null
    val people = wiki
        .filter { it.category == "인물" }
        .map { PersonHit(name = it.title, email = "", messageCount = 0, lastSubject = it.snippet, wikiPath = it.path) }
    return SearchResults(wiki = wiki, diary = diary, people = people)
}

/** Merged people directory (`miniapp.people.list`): recent Gmail counterparties
 *  ranked by message volume, with their 인물 wiki page folded in when matched,
 *  plus wiki-only people (no recent mail) appended by the gateway. Null on a
 *  fetch failure so the screen can offer retry instead of a misleading "empty".
 *  Session-cached so re-entering the section within the TTL skips the network. */
suspend fun DenebGatewayClient.fetchPeople(force: Boolean = false): List<PersonHit>? {
    return sectionCaches.people.getOrLoad(force) {
        val p = callRpc<PeopleListPayload>(
            "miniapp.people.list",
            buildJsonObject { put("limit", 60) },
        ) ?: return@getOrLoad null
        p.people
            .filter { it.email.isNotBlank() || it.name.isNotBlank() }
            .distinctByLast { it.email.ifBlank { it.name } }
            .map {
                PersonHit(
                    name = it.name.ifBlank { it.email },
                    email = it.email,
                    messageCount = it.messageCount,
                    lastSubject = it.lastSubject,
                    wikiPath = it.wikiPath,
                    wikiSummary = it.wikiSummary,
                )
            }
    }
}

/** The full device address book (miniapp.contacts.list) for the 전체 연락처 browser:
 *  every synced contact (name + phones/emails/org), unsorted — the screen sections it
 *  alphabetically (ㄱㄴㄷ). Null on transport/auth failure or an unconfigured store.
 *  Session-cached so re-entering the section within the TTL skips the network. */
suspend fun DenebGatewayClient.fetchContacts(force: Boolean = false): List<ContactRow>? = sectionCaches.contacts.getOrLoad(force) {
    callRpc<ContactsListPayload>(
        "miniapp.contacts.list",
        buildJsonObject {},
    )?.contacts?.filter { it.name.isNotBlank() }
}

/** miniapp.contacts.dedup — the deterministic dedup preview (how many entries collapse
 *  to how many people, plus the safe merge groups). Computed fresh each call rather
 *  than session-cached: it is an action result, and the operator runs it deliberately.
 *  Null on transport/auth failure or an unconfigured store. */
internal suspend fun DenebGatewayClient.fetchContactsDedup(): ContactsDedupPayload? = callRpc<ContactsDedupPayload>(
    "miniapp.contacts.dedup",
    buildJsonObject {},
)
