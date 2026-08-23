package ai.deneb.deneb

import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** Request and state contracts for wiki, diary, search, people, and contacts. */
class GatewayKnowledgeRpcContractTest {
    @Test
    fun refreshMemoriesFiltersBlankPathsDeduplicatesByLastAndMapsFallbackContent() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "pages":[
                    {"path":"","title":"must drop","summary":"bad"},
                    {"path":"wiki/a.md","title":"Old title","summary":"old summary","updated":"old"},
                    {"path":"wiki/b.md","title":"B title","summary":"","updated":"now"},
                    {"path":"wiki/c.md","title":"","summary":"","updated":"now"},
                    {"path":"wiki/a.md","title":"New title","summary":"new summary","updated":"new"}
                ]
            }
            """.trimIndent(),
        )

        f.client.refreshMemories()

        val params = f.transport.singleRequest().requireRpc("miniapp.memory.list_in_category")
        assertEquals("", params["category"]?.jsonPrimitive?.content)
        assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(listOf("wiki/b.md", "wiki/c.md", "wiki/a.md"), f.client.denebMemories.value.map { it.key })
        assertEquals(listOf("B title", "wiki/c.md", "new summary"), f.client.denebMemories.value.map { it.content })
        assertTrue(f.client.denebMemories.value.all { it.createdAt == 0L })
        assertTrue(f.client.denebMemories.value.all { it.updatedAt == 0L })
    }

    @Test
    fun refreshMemoriesLeavesPriorSnapshotUntouchedOnFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"pages":[{"path":"wiki/stable.md","summary":"stable"}]}""")
        f.client.refreshMemories()
        f.transport.enqueueJson("not-json")

        f.client.refreshMemories()

        assertEquals(listOf("wiki/stable.md"), f.client.denebMemories.value.map { it.key })
        assertEquals(listOf("stable"), f.client.denebMemories.value.map { it.content })
    }

    @Test
    fun refreshMemoriesPublishesValidEmptySnapshot() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"pages":[{"path":"wiki/a.md","summary":"A"}]}""")
        f.client.refreshMemories()
        f.transport.enqueueRpc("""{"pages":[]}""")

        f.client.refreshMemories()

        assertTrue(f.client.denebMemories.value.isEmpty())
    }

    @Test
    fun fetchCategoriesMapsCountsAndCorpusTotals() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "categories":[
                    {"name":"projects","pageCount":17},
                    {"name":"people","pageCount":2147483647},
                    {"name":"","pageCount":0}
                ],
                "totalPages":2147483647,
                "totalBytes":9223372036854775807
            }
            """.trimIndent(),
        )

        val categories = f.client.fetchCategories()

        val request = f.transport.singleRequest()
        assertEquals("miniapp.memory.categories", request.rpcMethod)
        assertTrue(request.rpcParams?.isEmpty() == true)
        assertEquals(listOf("projects", "people", ""), categories?.categories?.map { it.name })
        assertEquals(listOf(17, Int.MAX_VALUE, 0), categories?.categories?.map { it.pageCount })
        assertEquals(Int.MAX_VALUE, categories?.totalPages)
        assertEquals(Long.MAX_VALUE, categories?.totalBytes)
    }

    @Test
    fun fetchCategoriesReturnsValidEmptyCorpus() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        val categories = f.client.fetchCategories()

        assertNotNull(categories)
        assertTrue(categories.categories.isEmpty())
        assertEquals(0, categories.totalPages)
        assertEquals(0L, categories.totalBytes)
    }

    @Test
    fun fetchCategoryPagesFiltersBlankPathsKeepsLastDuplicateAndFallsBackTitle() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "pages":[
                    {"path":"projects/a.md","title":"Old","summary":"old","updated":"1"},
                    {"path":"","title":"Drop","summary":"drop","updated":"2"},
                    {"path":"projects/b.md","title":"","summary":"B summary","updated":"3"},
                    {"path":"projects/a.md","title":"New","summary":"new","updated":"4"}
                ]
            }
            """.trimIndent(),
        )

        val pages = f.client.fetchCategoryPages("projects")

        val params = f.transport.singleRequest().requireRpc("miniapp.memory.list_in_category")
        assertEquals("projects", params["category"]?.jsonPrimitive?.content)
        assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(listOf("projects/b.md", "projects/a.md"), pages?.map { it.path })
        assertEquals(listOf("projects/b.md", "New"), pages?.map { it.title })
        assertEquals(listOf("B summary", "new"), pages?.map { it.summary })
        assertEquals(listOf("3", "4"), pages?.map { it.updated })
    }

    @Test
    fun fetchCategoryPagesPreservesUnicodeAndBlankCategory() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"pages":[]}""")

        val pages = f.client.fetchCategoryPages("")

        assertNotNull(pages)
        assertTrue(pages.isEmpty())
        assertEquals("", f.transport.singleRequest().rpcParams?.get("category")?.jsonPrimitive?.content)
    }

    @Test
    fun fetchRecentDiarySendsCallerLimitAndMapsEntriesInOrder() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "entries":[
                    {"file":"memory/2026-07-11.md","header":"09:00","content":"회의","at":100},
                    {"file":"memory/2026-07-10.md","header":"어제","content":"결정","at":50}
                ]
            }
            """.trimIndent(),
        )

        val entries = f.client.fetchRecentDiary(limit = 2147483647)

        val params = f.transport.singleRequest().requireRpc("miniapp.memory.diary_recent")
        assertEquals(Int.MAX_VALUE, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(listOf("09:00", "어제"), entries?.map { it.header })
        assertEquals(listOf("회의", "결정"), entries?.map { it.content })
        assertEquals(listOf("memory/2026-07-11.md", "memory/2026-07-10.md"), entries?.map { it.file })
    }

    @Test
    fun fetchRecentDiaryUsesThirtyAsDefaultLimit() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"entries":[]}""")

        val entries = f.client.fetchRecentDiary()

        assertNotNull(entries)
        assertEquals(30, f.transport.singleRequest().rpcParams?.get("limit")?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun deleteCategoryPagesSkipsNetworkForEmptyInput() = runTest {
        val f = gatewayClientFixture()

        val deleted = f.client.deleteCategoryPages(emptyList())

        assertTrue(deleted)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun deleteCategoryPagesDeduplicatesPathsAndRequiresExactDeleteCount() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"deleted":2}""")

        val deleted = f.client.deleteCategoryPages(
            listOf("wiki/a.md", "wiki/a.md", "wiki/b.md", "wiki/b.md"),
        )

        assertTrue(deleted)
        val paths = f.transport.singleRequest()
            .requireRpc("miniapp.memory.delete_pages")["paths"]
            ?.jsonArray
            ?.map { it.jsonPrimitive.content }
        assertEquals(listOf("wiki/a.md", "wiki/b.md"), paths)
    }

    @Test
    fun deleteCategoryPagesRejectsPartialSuccessEvenWhenEnvelopeOk() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"deleted":1}""")

        val deleted = f.client.deleteCategoryPages(listOf("a.md", "b.md"))

        assertFalse(deleted)
    }

    @Test
    fun deleteCategoryPagesRejectsMatchingCountWhenPayloadOkFalse() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":false,"deleted":2}""")

        val deleted = f.client.deleteCategoryPages(listOf("a.md", "b.md"))

        assertFalse(deleted)
    }

    @Test
    fun deleteCategoryPagesRejectsOverReportedCount() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"deleted":3}""")

        val deleted = f.client.deleteCategoryPages(listOf("a.md", "b.md"))

        assertFalse(deleted)
    }

    @Test
    fun moveWikiPagePreservesExactFromAndToPaths() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true}""")

        val moved = f.client.moveWikiPage("Projects/Case A.md", "archive/Case A.md")

        assertTrue(moved)
        val params = f.transport.singleRequest().requireRpc("miniapp.memory.move_page")
        assertEquals("Projects/Case A.md", params["from"]?.jsonPrimitive?.content)
        assertEquals("archive/Case A.md", params["to"]?.jsonPrimitive?.content)
        assertEquals(2, params.size)
    }

    @Test
    fun moveWikiPageReturnsFalseForRejectedAndMalformedResponses() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":false}""")
        f.transport.enqueueJson("not-json")

        val rejected = f.client.moveWikiPage("a", "b")
        val malformed = f.client.moveWikiPage("c", "d")

        assertFalse(rejected)
        assertFalse(malformed)
    }

    @Test
    fun moveCategoryPagesDeduplicatesSkipsAlreadyFiledAndCountsOnlySuccesses() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true}""")
        f.transport.enqueueRpc("""{"ok":false}""")

        val moved = f.client.moveCategoryPages(
            paths = listOf(
                "inbox/a.md",
                "inbox/a.md",
                "target/b.md",
                "other/c.md",
            ),
            targetCategory = "target",
        )

        assertEquals(1, moved)
        assertEquals(2, f.transport.requests.size)
        val first = f.transport.requests[0].requireRpc("miniapp.memory.move_page")
        assertEquals("inbox/a.md", first["from"]?.jsonPrimitive?.content)
        assertEquals("target/a.md", first["to"]?.jsonPrimitive?.content)
        val second = f.transport.requests[1].requireRpc("miniapp.memory.move_page")
        assertEquals("other/c.md", second["from"]?.jsonPrimitive?.content)
        assertEquals("target/c.md", second["to"]?.jsonPrimitive?.content)
    }

    @Test
    fun moveCategoryPagesHandlesRootBasenameWithoutDroppingIt() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true}""")

        val moved = f.client.moveCategoryPages(listOf("page.md"), "projects")

        assertEquals(1, moved)
        val params = f.transport.singleRequest().requireRpc("miniapp.memory.move_page")
        assertEquals("page.md", params["from"]?.jsonPrimitive?.content)
        assertEquals("projects/page.md", params["to"]?.jsonPrimitive?.content)
    }

    @Test
    fun fetchWikiPageMapsFullFrontmatterAndBody() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "path":"projects/namdo.md",
                "title":"남도에코",
                "summary":"투자 검토 프로젝트",
                "category":"projects",
                "code":"ND-2026",
                "tags":["투자","실사"],
                "related":["mail:7"],
                "updated":"2026-07-11T01:00:00Z",
                "body":"# 남도에코\n본문"
            }
            """.trimIndent(),
        )

        val page = f.client.fetchWikiPage("projects/namdo.md")

        val params = f.transport.singleRequest().requireRpc("miniapp.memory.get_page")
        assertEquals("projects/namdo.md", params["path"]?.jsonPrimitive?.content)
        assertEquals("projects/namdo.md", page?.path)
        assertEquals("남도에코", page?.title)
        assertEquals("투자 검토 프로젝트", page?.summary)
        assertEquals("projects", page?.category)
        assertEquals("ND-2026", page?.code)
        assertEquals(listOf("투자", "실사"), page?.tags)
        assertEquals("2026-07-11T01:00:00Z", page?.updated)
        assertEquals("# 남도에코\n본문", page?.body)
    }

    @Test
    fun fetchWikiPageFallsBackToPathForBlankTitle() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"path":"wiki/untitled.md","title":"","body":"body"}""")

        val page = f.client.fetchWikiPage("wiki/untitled.md")

        assertEquals("wiki/untitled.md", page?.title)
    }

    @Test
    fun saveWikiPageOmitsAbsentOptionalFrontmatter() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        val saved = f.client.saveWikiPage("wiki/a.md", "body")

        assertTrue(saved)
        val params = f.transport.singleRequest().requireRpc("miniapp.memory.write_page")
        assertEquals("wiki/a.md", params["path"]?.jsonPrimitive?.content)
        assertEquals("body", params["body"]?.jsonPrimitive?.content)
        assertFalse(params.containsKey("title"))
        assertFalse(params.containsKey("summary"))
        assertFalse(params.containsKey("tags"))
        assertEquals(2, params.size)
    }

    @Test
    fun saveWikiPageIncludesExplicitBlankAndEmptyOptionalFrontmatter() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        val saved = f.client.saveWikiPage(
            path = "wiki/a.md",
            body = "",
            title = "",
            summary = "",
            tags = emptyList(),
        )

        assertTrue(saved)
        val params = f.transport.singleRequest().requireRpc("miniapp.memory.write_page")
        assertTrue(params.containsKey("title"))
        assertEquals("", params["title"]?.jsonPrimitive?.content)
        assertTrue(params.containsKey("summary"))
        assertEquals("", params["summary"]?.jsonPrimitive?.content)
        assertTrue(params.containsKey("tags"))
        assertTrue(params["tags"]?.jsonArray?.isEmpty() == true)
    }

    @Test
    fun saveWikiPagePreservesOrderedUnicodeTags() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        f.client.saveWikiPage(
            path = "people/홍길동.md",
            body = "인물 정보",
            tags = listOf("임원", "광주", "A&B"),
        )

        val tags = f.transport.singleRequest()
            .requireRpc("miniapp.memory.write_page")["tags"]
            ?.jsonArray
            ?.map { it.jsonPrimitive.content }
        assertEquals(listOf("임원", "광주", "A&B"), tags)
    }

    @Test
    fun saveWikiPageReturnsFalseWhenGatewayRejectsWrite() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(payload = "{}", ok = false)

        val saved = f.client.saveWikiPage("wiki/a.md", "body")

        assertFalse(saved)
    }

    @Test
    fun createWikiPageSendsAllFieldsAndReturnsServerChosenPath() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"path":"projects/generated-slug.md","title":"새 프로젝트"}""")

        val path = f.client.createWikiPage(
            title = "새 프로젝트",
            category = "projects",
            body = "# 새 프로젝트\n초안",
        )

        assertEquals("projects/generated-slug.md", path)
        val params = f.transport.singleRequest().requireRpc("miniapp.memory.create_page")
        assertEquals("새 프로젝트", params["title"]?.jsonPrimitive?.content)
        assertEquals("projects", params["category"]?.jsonPrimitive?.content)
        assertEquals("# 새 프로젝트\n초안", params["body"]?.jsonPrimitive?.content)
        assertEquals(3, params.size)
    }

    @Test
    fun createWikiPageRejectsBlankServerPath() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"path":"   ","title":"No path"}""")

        val path = f.client.createWikiPage("No path", "wiki", "body")

        assertNull(path)
    }

    @Test
    fun searchAllMapsEverySourceAndPreservesGroundingLocators() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "wiki":[
                    {"path":"wiki/a.md","title":"A","summary":"summary","category":"wiki","snippet":"matched text","score":0.9},
                    {"path":"wiki/b.md","title":"","summary":"fallback summary","category":"projects","snippet":"","score":0.8},
                    {"path":"","title":"drop","summary":"drop","category":"wiki","snippet":"drop"}
                ],
                "diary":[
                    {"file":"memory/day.md","header":"","content":"diary content","at":10,"score":0.7},
                    {"file":"memory/day.md","header":"결정","content":"ship it","at":11,"score":0.6}
                ],
                "people":[
                    {"email":"ceo@example.com","name":"대표","messageCount":9,"lastSubject":"계약"},
                    {"email":"person@example.com","name":"","messageCount":3,"lastSubject":"안부"},
                    {"email":"","name":"","messageCount":999,"lastSubject":"drop"}
                ],
                "files":[
                    {"path":"docs/quote.pdf","name":"quote.pdf","size":245760,"snippet":"공급가액 3억원","score":0.93,"startLine":31,"endLine":35,"kind":"pdf","heading":"견적 합계"},
                    {"path":"","name":"drop.pdf","snippet":"drop","score":0.1}
                ],
                "mail":[
                    {"id":"mail-1","threadId":"thread-1","from":"협력사","subject":"수정 견적","date":"2026-08-23","snippet":"최종 금액입니다","mailbox":"INBOX"},
                    {"id":"","subject":"drop"}
                ],
                "sources":{"wiki":"ok","diary":"ok","people":"timeout","files":"ok","mail":"error"}
            }
            """.trimIndent(),
        )

        val results = f.client.searchAll("투자 계약")

        val params = f.transport.singleRequest().requireRpc("miniapp.search.all")
        assertEquals("투자 계약", params["query"]?.jsonPrimitive?.content)
        assertEquals(20, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(listOf("wiki/a.md", "wiki/b.md"), results?.wiki?.map { it.path })
        assertEquals(listOf("A", "wiki/b.md"), results?.wiki?.map { it.title })
        assertEquals(listOf("matched text", "fallback summary"), results?.wiki?.map { it.snippet })
        assertEquals(listOf("일기", "결정"), results?.diary?.map { it.title })
        assertEquals(listOf("diary content", "ship it"), results?.diary?.map { it.snippet })
        assertEquals(listOf("memory/day.md", "memory/day.md"), results?.diary?.map { it.path })
        assertEquals(listOf("대표", "person@example.com"), results?.people?.map { it.name })
        assertEquals(listOf(9, 3), results?.people?.map { it.messageCount })
        assertEquals(listOf("docs/quote.pdf"), results?.files?.map { it.path })
        assertEquals(245_760L, results?.files?.single()?.size)
        assertEquals(31, results?.files?.single()?.startLine)
        assertEquals(35, results?.files?.single()?.endLine)
        assertEquals("견적 합계", results?.files?.single()?.heading)
        assertEquals(listOf("mail-1"), results?.mail?.map { it.id })
        assertEquals("thread-1", results?.mail?.single()?.threadId)
        assertEquals("timeout", results?.sourceStatus?.people)
        assertEquals("error", results?.sourceStatus?.mail)
    }

    @Test
    fun searchAllReturnsEmptyCollectionsForValidEmptyResult() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        val results = f.client.searchAll("none")

        assertNotNull(results)
        assertTrue(results.wiki.isEmpty())
        assertTrue(results.diary.isEmpty())
        assertTrue(results.people.isEmpty())
        assertTrue(results.files.isEmpty())
        assertTrue(results.mail.isEmpty())
    }

    @Test
    fun fetchPeopleFiltersIdentitylessRowsDeduplicatesByLastAndMapsWikiFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "people":[
                    {"email":"a@example.com","name":"Old A","messageCount":1,"lastSubject":"old"},
                    {"email":"","name":"Wiki Only","messageCount":0,"wikiPath":"people/wiki-only.md","wikiSummary":"wiki"},
                    {"email":"","name":"","messageCount":99},
                    {"email":"a@example.com","name":"New A","messageCount":7,"lastSubject":"new","wikiPath":"people/a.md","wikiSummary":"merged"},
                    {"email":"fallback@example.com","name":"","messageCount":2,"lastSubject":"hello"}
                ]
            }
            """.trimIndent(),
        )

        val people = f.client.fetchPeople()

        val params = f.transport.singleRequest().requireRpc("miniapp.people.list")
        assertEquals(60, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(listOf("Wiki Only", "New A", "fallback@example.com"), people?.map { it.name })
        assertEquals(listOf("", "a@example.com", "fallback@example.com"), people?.map { it.email })
        assertEquals(listOf(0, 7, 2), people?.map { it.messageCount })
        assertEquals("people/a.md", people?.get(1)?.wikiPath)
        assertEquals("merged", people?.get(1)?.wikiSummary)
    }

    @Test
    fun fetchContactsFiltersBlankNamesAndPreservesMultipleChannels() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "contacts":[
                    {"name":"홍길동","phones":["010-1111-2222","062-123-4567"],"emails":["hong@example.com"],"org":"Deneb"},
                    {"name":"","phones":["010-0000-0000"],"emails":[],"org":"Hidden"},
                    {"name":"Alice","phones":[],"emails":["a@example.com","alice@work.example"],"org":"A&B"}
                ]
            }
            """.trimIndent(),
        )

        val contacts = f.client.fetchContacts()

        assertEquals("miniapp.contacts.list", f.transport.singleRequest().rpcMethod)
        assertTrue(f.transport.singleRequest().rpcParams?.isEmpty() == true)
        assertEquals(listOf("홍길동", "Alice"), contacts?.map { it.name })
        assertEquals(listOf("010-1111-2222", "062-123-4567"), contacts?.first()?.phones)
        assertEquals(listOf("hong@example.com"), contacts?.first()?.emails)
        assertEquals("Deneb", contacts?.first()?.org)
        assertEquals(listOf("a@example.com", "alice@work.example"), contacts?.last()?.emails)
    }

    @Test
    fun knowledgeReadsReturnNullOnHttpFailureRatherThanEmptyData() = runTest {
        val f = gatewayClientFixture()
        repeat(7) {
            f.transport.enqueueRpc(payload = "{}", status = HttpStatusCode.BadGateway)
        }

        val categories = f.client.fetchCategories()
        val pages = f.client.fetchCategoryPages("wiki")
        val diary = f.client.fetchRecentDiary()
        val page = f.client.fetchWikiPage("wiki/a.md")
        val search = f.client.searchAll("query")
        val people = f.client.fetchPeople()
        val contacts = f.client.fetchContacts()

        assertNull(categories)
        assertNull(pages)
        assertNull(diary)
        assertNull(page)
        assertNull(search)
        assertNull(people)
        assertNull(contacts)
    }

    @Test
    fun knowledgeRpcCancellationPropagatesWithoutPublishingPartialState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"pages":[{"path":"stable.md","summary":"stable"}]}""")
        f.client.refreshMemories()
        f.transport.enqueueFailure(CancellationException("cancel knowledge"))

        val failure = assertFailsWith<CancellationException> {
            f.client.refreshMemories()
        }

        assertEquals("cancel knowledge", failure.message)
        assertEquals(listOf("stable.md"), f.client.denebMemories.value.map { it.key })
    }
}
