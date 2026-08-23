package ai.deneb.deneb

import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.MemoryCategoryRow
import ai.deneb.deneb.generated.MemoryPageRow
import ai.deneb.deneb.generated.PersonRow
import ai.deneb.deneb.generated.SearchAllResult
import ai.deneb.deneb.generated.SearchDiaryHit
import ai.deneb.deneb.generated.SearchWikiHit
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class MemoryWikiSearchBoundaryTest {

    private val json = Json { encodeDefaults = true }

    private fun page(
        path: String,
        title: String = "",
        summary: String = "",
        updated: String = "",
    ) = MemoryPageRow(path = path, title = title, summary = summary, updated = updated)

    private fun pagesPayload(vararg pages: MemoryPageRow): String = json.encodeToString(MemoryListPayload(pages.toList()))

    @Test
    fun memoryRefreshRequestsRootCategoryWithHardLimit() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(pagesPayload())

        f.client.refreshMemories()

        val params = f.transport.singleRequest().requireRpc("miniapp.memory.list_in_category")
        assertEquals("", params["category"]?.jsonPrimitive?.content)
        assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun memoryRefreshFiltersBlankAndDeduplicatesPaths() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            pagesPayload(
                page(""),
                page("projects/one.md", summary = "old"),
                page("projects/one.md", summary = "new"),
                page("people/two.md"),
            ),
        )

        f.client.refreshMemories()

        assertEquals(listOf("projects/one.md", "people/two.md"), f.client.denebMemories.value.map { it.key })
        assertEquals("new", f.client.denebMemories.value.first().content)
    }

    @Test
    fun memoryContentFallsBackSummaryThenTitleThenPath() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            pagesPayload(
                page("one", title = "Title", summary = "Summary"),
                page("two", title = "Title two"),
                page("three"),
            ),
        )

        f.client.refreshMemories()

        assertEquals(listOf("Summary", "Title two", "three"), f.client.denebMemories.value.map { it.content })
    }

    @Test
    fun failedMemoryRefreshPreservesSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.client._denebMemories.value = listOf(
            ai.deneb.data.MemoryEntry(key = "cached", content = "Cached", createdAt = 0, updatedAt = 0),
        )
        f.transport.enqueueJson("bad")

        f.client.refreshMemories()

        assertEquals("cached", f.client.denebMemories.value.single().key)
    }

    @Test
    fun categorySummaryMapsCountsAndCorpusTotals() = runTest {
        val f = gatewayClientFixture()
        val payload = CategoriesPayload(
            categories = listOf(
                MemoryCategoryRow(name = "projects", pageCount = 7),
                MemoryCategoryRow(name = "people", pageCount = 3),
            ),
            totalPages = 10,
            totalBytes = 12_345,
        )
        f.transport.enqueueRpc(json.encodeToString(payload))

        val result = f.client.fetchCategories()

        assertEquals(listOf("projects", "people"), result?.categories?.map { it.name })
        assertEquals(listOf(7, 3), result?.categories?.map { it.pageCount })
        assertEquals(10, result?.totalPages)
        assertEquals(12_345, result?.totalBytes)
    }

    @Test
    fun categoryPagesSendExactCategoryAndLimit() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(pagesPayload(page("projects/one.md", title = "One", summary = "Summary", updated = "today")))

        val result = f.client.fetchCategoryPages("projects")

        assertEquals("One", result?.single()?.title)
        assertEquals("Summary", result?.single()?.summary)
        assertEquals("today", result?.single()?.updated)
        val params = f.transport.singleRequest().requireRpc("miniapp.memory.list_in_category")
        assertEquals("projects", params["category"]?.jsonPrimitive?.content)
        assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun categoryPagesFilterBlankDeduplicateAndFallbackTitle() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            pagesPayload(
                page(""),
                page("projects/one.md", title = "old"),
                page("projects/one.md", title = "new"),
                page("projects/two.md"),
            ),
        )

        val result = f.client.fetchCategoryPages("projects").orEmpty()

        assertEquals(listOf("projects/one.md", "projects/two.md"), result.map { it.path })
        assertEquals(listOf("new", "projects/two.md"), result.map { it.title })
    }

    @Test
    fun recentDiaryPassesCallerLimitAndMapsEntries() = runTest {
        val f = gatewayClientFixture()
        val payload = DiaryRecentPayload(
            entries = listOf(DiaryRecentRow(file = "2026-07-11.md", header = "Today", content = "Progress", at = 10)),
        )
        f.transport.enqueueRpc(json.encodeToString(payload))

        val result = f.client.fetchRecentDiary(limit = 7)

        assertEquals("Today", result?.single()?.header)
        assertEquals("Progress", result?.single()?.content)
        assertEquals("2026-07-11.md", result?.single()?.file)
        assertEquals(7, f.transport.singleRequest().rpcParams?.get("limit")?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun emptyDeleteSelectionSucceedsWithoutNetwork() = runTest {
        val f = gatewayClientFixture()

        assertTrue(f.client.deleteCategoryPages(emptyList()))

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun deletePagesDeduplicatesPathsBeforeRequestAndCountCheck() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(DeletePagesPayload(ok = true, deleted = 2)))

        val result = f.client.deleteCategoryPages(listOf("one", "one", "two"))

        assertTrue(result)
        val paths = f.transport.singleRequest().rpcParams?.get("paths")?.jsonArray?.map { it.jsonPrimitive.content }
        assertEquals(listOf("one", "two"), paths)
    }

    @Test
    fun deletePagesReportsPartialAndEnvelopeFailure() = runTest {
        val partial = gatewayClientFixture()
        partial.transport.enqueueRpc(json.encodeToString(DeletePagesPayload(ok = true, deleted = 1)))
        assertFalse(partial.client.deleteCategoryPages(listOf("one", "two")))

        val rejected = gatewayClientFixture()
        rejected.transport.enqueueRpc(json.encodeToString(DeletePagesPayload(ok = false, deleted = 2)))
        assertFalse(rejected.client.deleteCategoryPages(listOf("one", "two")))
    }

    @Test
    fun movePageSendsExactSourceAndDestination() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(MovePagePayload(ok = true)))

        assertTrue(f.client.moveWikiPage("inbox/deal.md", "projects/deal.md"))

        val params = f.transport.singleRequest().requireRpc("miniapp.memory.move_page")
        assertEquals("inbox/deal.md", params["from"]?.jsonPrimitive?.content)
        assertEquals("projects/deal.md", params["to"]?.jsonPrimitive?.content)
    }

    @Test
    fun batchMoveSkipsAlreadyTargetedAndDeduplicatesInputs() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(MovePagePayload(ok = true)))
        f.transport.enqueueRpc(json.encodeToString(MovePagePayload(ok = true)))

        val moved = f.client.moveCategoryPages(
            listOf("projects/already.md", "inbox/one.md", "inbox/one.md", "people/two.md"),
            "projects",
        )

        assertEquals(2, moved)
        assertEquals(2, f.transport.requests.size)
        assertEquals(listOf("projects/one.md", "projects/two.md"), f.transport.requests.map { it.rpcParams?.get("to")?.jsonPrimitive?.content })
    }

    @Test
    fun batchMoveCountsOnlyAcceptedMoves() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(MovePagePayload(ok = true)))
        f.transport.enqueueRpc(json.encodeToString(MovePagePayload(ok = false)))
        f.transport.enqueueJson("bad")

        val moved = f.client.moveCategoryPages(listOf("a/one", "a/two", "a/three"), "b")

        assertEquals(1, moved)
    }

    @Test
    fun fetchWikiPageMapsMetadataAndTitleFallback() = runTest {
        val f = gatewayClientFixture()
        val payload = WikiPagePayload(
            path = "projects/one.md",
            title = "",
            summary = "Summary",
            category = "projects",
            code = "P-001",
            tags = listOf("deal", "active"),
            updated = "today",
            body = "# Body",
        )
        f.transport.enqueueRpc(json.encodeToString(payload))

        val result = f.client.fetchWikiPage("projects/one.md")

        assertEquals("projects/one.md", result?.title)
        assertEquals("Summary", result?.summary)
        assertEquals("projects", result?.category)
        assertEquals("P-001", result?.code)
        assertEquals(listOf("deal", "active"), result?.tags)
        assertEquals("# Body", result?.body)
    }

    @Test
    fun saveWikiPageOmitsNullFrontmatterButIncludesExplicitEmptyValues() = runTest {
        val omit = gatewayClientFixture()
        omit.transport.enqueueRpc("{}")
        assertTrue(omit.client.saveWikiPage("one.md", "body"))
        assertEquals(setOf("path", "body"), omit.transport.singleRequest().rpcParams.orEmpty().keys)

        val clear = gatewayClientFixture()
        clear.transport.enqueueRpc("{}")
        assertTrue(clear.client.saveWikiPage("one.md", "body", title = "", summary = "", tags = emptyList()))
        val params = clear.transport.singleRequest().rpcParams.orEmpty()
        assertEquals("", params["title"]?.jsonPrimitive?.content)
        assertEquals("", params["summary"]?.jsonPrimitive?.content)
        assertTrue(params["tags"]?.jsonArray?.isEmpty() == true)
    }

    @Test
    fun createWikiPageSendsFieldsAndRejectsBlankReturnedPath() = runTest {
        val success = gatewayClientFixture()
        success.transport.enqueueRpc(json.encodeToString(WikiPagePayload(path = "projects/new.md")))
        assertEquals("projects/new.md", success.client.createWikiPage("New", "projects", "Body"))
        val params = success.transport.singleRequest().requireRpc("miniapp.memory.create_page")
        assertEquals("New", params["title"]?.jsonPrimitive?.content)
        assertEquals("projects", params["category"]?.jsonPrimitive?.content)
        assertEquals("Body", params["body"]?.jsonPrimitive?.content)

        val blank = gatewayClientFixture()
        blank.transport.enqueueRpc(json.encodeToString(WikiPagePayload(path = "")))
        assertNull(blank.client.createWikiPage("New", "projects", "Body"))
    }

    @Test
    fun unifiedSearchMapsWikiDiaryAndPeopleFallbacks() = runTest {
        val f = gatewayClientFixture()
        val payload = SearchAllResult(
            wiki = listOf(
                SearchWikiHit(path = "", title = "drop"),
                SearchWikiHit(path = "wiki/one", title = "", snippet = "Snippet", summary = "Summary", category = "wiki"),
                SearchWikiHit(path = "wiki/two", title = "Two", snippet = "", summary = "Fallback", category = "wiki"),
            ),
            diary = listOf(SearchDiaryHit(header = "", content = "Diary")),
            people = listOf(
                PersonRow(),
                PersonRow(email = "a@example.com", name = "", messageCount = 4, lastSubject = "Hello"),
            ),
        )
        f.transport.enqueueRpc(json.encodeToString(payload))

        val result = f.client.searchAll("query")

        assertEquals(listOf("wiki/one", "wiki/two"), result?.wiki?.map { it.path })
        assertEquals(listOf("wiki/one", "Two"), result?.wiki?.map { it.title })
        assertEquals(listOf("Snippet", "Fallback"), result?.wiki?.map { it.snippet })
        assertEquals("일기", result?.diary?.single()?.title)
        assertEquals("a@example.com", result?.people?.single()?.name)
        assertEquals(4, result?.people?.single()?.messageCount)
    }

    @Test
    fun unifiedSearchPreservesFactIdentityAndDropsCandidateTimeValue() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            json.encodeToString(
                SearchAllResult(
                    wiki = listOf(
                        SearchWikiHit(
                            path = "@facts/fact-123.md",
                            snippet = "교정 전 값이 남을 수 있는 검색 스니펫",
                            resultKind = "fact",
                            readOnly = true,
                            factId = "fact-123",
                            subjectId = "project:alpha",
                        ),
                    ),
                ),
            ),
        )

        val hit = assertNotNull(f.client.searchAll("alpha")?.wiki?.single())

        assertEquals("project:alpha", hit.title)
        assertEquals("", hit.snippet)
        assertEquals("fact", hit.resultKind)
        assertTrue(hit.readOnly)
        assertEquals("fact-123", hit.factId)
        assertEquals("project:alpha", hit.subjectId)
        assertTrue(hit.isCurrentFactHit())
    }

    @Test
    fun currentFactReadAlwaysHitsGatewayAndNeverUsesWikiSectionCache() = runTest {
        val f = gatewayClientFixture()
        val ref = "@facts/fact-123.md"
        val poisonedCache = WikiPage(
            path = ref,
            title = "stale cache",
            summary = "",
            category = "fact-plane",
            tags = emptyList(),
            updated = "",
            body = "stale",
        )
        f.client.sectionCaches.wikiPages.getOrLoad(ref) { poisonedCache }
        f.transport.enqueueRpc(json.encodeToString(WikiPagePayload(path = ref, title = "Current one", body = "first")))
        f.transport.enqueueRpc(json.encodeToString(WikiPagePayload(path = ref, title = "Current two", body = "second")))

        val first = f.client.fetchCurrentFactPage(ref)
        val second = f.client.fetchCurrentFactPage(ref)

        assertEquals("first", first?.body)
        assertEquals("second", second?.body)
        assertEquals("stale", f.client.sectionCaches.wikiPages.peek(ref)?.body)
        assertEquals(listOf("miniapp.memory.get_page", "miniapp.memory.get_page"), f.transport.requestMethods())
        assertTrue(f.transport.requests.all { it.rpcParams?.get("path")?.jsonPrimitive?.content == ref })
    }

    @Test
    fun pathOnlyBackslashFactHitUsesCanonicalUncachedReference() = runTest {
        val f = gatewayClientFixture()
        val hit = SearchHit(
            path = "@facts\\fact-123.md",
            title = "Current fact",
            snippet = "",
            category = "fact-plane",
        )
        val canonicalRef = "@facts/fact-123.md"
        f.transport.enqueueRpc(
            json.encodeToString(WikiPagePayload(path = canonicalRef, title = "Current fact", body = "current")),
        )

        assertTrue(hit.isCurrentFactHit())
        assertEquals("current", f.client.fetchCurrentFactPage(hit.path)?.body)
        assertEquals(canonicalRef, f.transport.requests.single().rpcParams?.get("path")?.jsonPrimitive?.content)
    }

    @Test
    fun currentFactReadFailsClosedForOldOrMalformedReferenceWithoutMirrorFallback() = runTest {
        val f = gatewayClientFixture()
        val oldRef = "@facts/fact-old.md"
        val staleMirrorPage = WikiPage(
            path = oldRef,
            title = "stale mirror",
            summary = "",
            category = "fact-plane",
            tags = emptyList(),
            updated = "",
            body = "forgotten value",
        )
        assertTrue(f.client.wikiMirror.replaceAll(listOf(staleMirrorPage), nowMs = 1))
        assertNull(f.client.wikiMirror.get(oldRef))
        // Even a successful envelope must match the exact immutable ref. A
        // replacement claim cannot be silently substituted for the old one.
        f.transport.enqueueRpc(
            json.encodeToString(WikiPagePayload(path = "@facts/fact-new.md", body = "replacement")),
        )

        assertNull(f.client.fetchCurrentFactPage(oldRef))
        assertNull(f.client.fetchCurrentFactPage("wiki/ordinary.md"))
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun syntheticFactRefsCannotEnterEditableWikiRpcSurface() = runTest {
        val f = gatewayClientFixture()
        for (ref in listOf("@facts/fact-123.md", "사용자\\현행-사실.md")) {
            assertNull(f.client.fetchWikiPage(ref))
            assertFalse(f.client.saveWikiPage(ref, "overwrite"))
            assertFalse(f.client.deleteCategoryPages(listOf(ref)))
            assertFalse(f.client.moveWikiPage(ref, "wiki/ordinary.md"))
            assertFalse(f.client.moveWikiPage("wiki/ordinary.md", ref))
        }
        assertNull(f.client.createWikiPage("Fact", "@facts", "body"))
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun generatedCurrentFactsProfileIsExcludedFromEditableListings() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            pagesPayload(
                page("사용자/현행-사실.md", title = "현행 사실"),
                page("사용자/프로필.md", title = "프로필"),
            ),
        )

        val result = f.client.fetchCategoryPages("사용자").orEmpty()

        assertEquals(listOf("사용자/프로필.md"), result.map { it.path })
    }

    @Test
    fun searchSendsQueryAndHardLimitTwenty() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(SearchAllResult()))

        f.client.searchAll("계약")

        val params = f.transport.singleRequest().requireRpc("miniapp.search.all")
        assertEquals("계약", params["query"]?.jsonPrimitive?.content)
        assertEquals(20, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun peopleListFiltersBlankAndDeduplicatesStableIdentity() = runTest {
        val f = gatewayClientFixture()
        val payload = PeopleListPayload(
            people = listOf(
                PersonRow(),
                PersonRow(email = "a@example.com", name = "Old"),
                PersonRow(email = "a@example.com", name = "New", wikiPath = "people/a.md", wikiSummary = "Summary"),
                PersonRow(name = "Wiki only", wikiPath = "people/wiki.md"),
            ),
        )
        f.transport.enqueueRpc(json.encodeToString(payload))

        val result = f.client.fetchPeople().orEmpty()

        assertEquals(2, result.size)
        assertEquals("New", result.first().name)
        assertEquals("people/a.md", result.first().wikiPath)
        assertEquals("Wiki only", result.last().name)
    }

    @Test
    fun peopleRequestUsesSixtyRowLimit() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(PeopleListPayload()))

        f.client.fetchPeople()

        assertEquals(60, f.transport.singleRequest().rpcParams?.get("limit")?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun contactsFilterBlankNamesAndPreserveAddressBookFields() = runTest {
        val f = gatewayClientFixture()
        val payload = ContactsListPayload(
            contacts = listOf(
                ContactRow(name = ""),
                ContactRow(name = "Alice", phones = listOf("010"), emails = listOf("a@example.com"), org = "Deneb"),
            ),
        )
        f.transport.enqueueRpc(json.encodeToString(payload))

        val result = f.client.fetchContacts().orEmpty()

        assertEquals(1, result.size)
        assertEquals(listOf("010"), result.single().phones)
        assertEquals(listOf("a@example.com"), result.single().emails)
        assertEquals("Deneb", result.single().org)
        f.transport.singleRequest().requireRpc("miniapp.contacts.list")
    }

    @Test
    fun knowledgeReadCancellationPropagates() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel memory"))

        val failure = assertFailsWith<CancellationException> { f.client.fetchCategories() }

        assertEquals("cancel memory", failure.message)
    }
}
