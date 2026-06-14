package ai.deneb.deneb

import ai.deneb.data.MemoryEntry
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
        .map { p ->
            MemoryEntry(
                key = p.path,
                content = p.summary.ifBlank { p.title.ifBlank { p.path } },
                createdAt = 0,
                updatedAt = 0,
            )
        }
}

/** All wiki categories with page counts + corpus totals (`memory.categories`). */
suspend fun DenebGatewayClient.fetchCategories(): WikiCategories? {
    val p = callRpc<CategoriesPayload>("miniapp.memory.categories", buildJsonObject {}) ?: return null
    return WikiCategories(
        categories = p.categories.map { WikiCategory(it.name, it.pageCount) },
        totalPages = p.totalPages,
        totalBytes = p.totalBytes,
    )
}

/** Pages within one category (`memory.list_in_category`); blank lists all.
 *  Null on a fetch failure so the screen can offer retry instead of showing a
 *  misleading "empty category". */
suspend fun DenebGatewayClient.fetchCategoryPages(category: String): List<WikiPageRef>? {
    val p = callRpc<MemoryListPayload>(
        "miniapp.memory.list_in_category",
        buildJsonObject {
            put("category", category)
            put("limit", 200)
        },
    ) ?: return null
    return p.pages
        .filter { it.path.isNotBlank() }
        .map { WikiPageRef(it.path, it.title.ifBlank { it.path }, it.summary, it.updated) }
}

/** Recent diary entries for the timeline (`miniapp.memory.diary_recent`).
 *  Null on a fetch failure so the screen can offer retry instead of showing
 *  a misleading empty timeline. */
suspend fun DenebGatewayClient.fetchRecentDiary(limit: Int = 30): List<DiaryEntry>? {
    val p = callRpc<DiaryRecentPayload>(
        "miniapp.memory.diary_recent",
        buildJsonObject { put("limit", limit) },
    ) ?: return null
    return p.entries.map { DiaryEntry(header = it.header, content = it.content, file = it.file) }
}

/** Delete one or more wiki pages by path (`miniapp.memory.delete_pages`).
 *  The backend deletes best-effort and reports a per-page failure list, so
 *  this returns true only when every requested page was actually removed —
 *  letting the category screen surface a partial failure instead of
 *  silently dropping unselected rows. */
suspend fun DenebGatewayClient.deleteCategoryPages(paths: List<String>): Boolean {
    if (paths.isEmpty()) return true
    val resp = callRpc<DeletePagesPayload>(
        "miniapp.memory.delete_pages",
        buildJsonObject {
            putJsonArray("paths") { paths.forEach { add(it) } }
        },
    ) ?: return false
    return resp.ok && resp.deleted == paths.size
}

/** Move one wiki page to a new path (`miniapp.memory.move_page`). The bucket is
 *  the path's leading directory, so this is how a page is reclassified. The
 *  backend rejects a traversal path, an invalid target category, or an existing
 *  target (no overwrite); returns true only when the move actually happened. */
suspend fun DenebGatewayClient.moveWikiPage(from: String, to: String): Boolean {
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
    for (p in paths) {
        val base = p.substringAfterLast('/')
        val to = "$targetCategory/$base"
        if (p == to) continue
        if (moveWikiPage(p, to)) moved++
    }
    return moved
}

/** Full wiki/memory page by path (`miniapp.memory.get_page`). */
suspend fun DenebGatewayClient.fetchWikiPage(path: String): WikiPage? {
    val p = callRpc<WikiPagePayload>(
        "miniapp.memory.get_page",
        buildJsonObject { put("path", path) },
    ) ?: return null
    return WikiPage(
        path = p.path,
        title = p.title.ifBlank { p.path },
        summary = p.summary,
        category = p.category,
        tags = p.tags,
        updated = p.updated,
        body = p.body,
    )
}

/** Overwrite a wiki page; non-null title/summary/tags also update frontmatter. */
suspend fun DenebGatewayClient.saveWikiPage(
    path: String,
    body: String,
    title: String? = null,
    summary: String? = null,
    tags: List<String>? = null,
): Boolean = callRpc<JsonObject>(
    "miniapp.memory.write_page",
    buildJsonObject {
        put("path", path)
        put("body", body)
        if (title != null) put("title", title)
        if (summary != null) put("summary", summary)
        if (tags != null) putJsonArray("tags") { tags.forEach { add(it) } }
    },
) != null

/** Create a new wiki page (`miniapp.memory.create_page`); returns its path. */
suspend fun DenebGatewayClient.createWikiPage(title: String, category: String, body: String): String? = callRpc<WikiPagePayload>(
    "miniapp.memory.create_page",
    buildJsonObject {
        put("title", title)
        put("category", category)
        put("body", body)
    },
)?.path

/** Unified search across wiki, diary and people (`miniapp.search.all`). */
suspend fun DenebGatewayClient.searchAll(query: String): SearchResults? {
    val p = callRpc<SearchAllResult>(
        "miniapp.search.all",
        buildJsonObject {
            put("query", query)
            put("limit", 20)
        },
    ) ?: return null
    return SearchResults(
        wiki = p.wiki.filter { it.path.isNotBlank() }
            .map { SearchHit(it.path, it.title.ifBlank { it.path }, it.snippet.ifBlank { it.summary }, it.category) },
        diary = p.diary.map { SearchHit("", it.header.ifBlank { "일기" }, it.content, "diary") },
        people = p.people.filter { it.email.isNotBlank() || it.name.isNotBlank() }
            .map { PersonHit(it.name.ifBlank { it.email }, it.email, it.messageCount, it.lastSubject) },
    )
}

/** Merged people directory (`miniapp.people.list`): recent Gmail counterparties
 *  ranked by message volume, with their 인물 wiki page folded in when matched,
 *  plus wiki-only people (no recent mail) appended by the gateway. Null on a
 *  fetch failure so the screen can offer retry instead of a misleading "empty". */
suspend fun DenebGatewayClient.fetchPeople(): List<PersonHit>? {
    val p = callRpc<PeopleListPayload>(
        "miniapp.people.list",
        buildJsonObject { put("limit", 60) },
    ) ?: return null
    return p.people
        .filter { it.email.isNotBlank() || it.name.isNotBlank() }
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
