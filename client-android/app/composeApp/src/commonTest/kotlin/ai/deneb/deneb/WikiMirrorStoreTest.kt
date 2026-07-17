package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** In-memory [WikiMirrorFiles] so the store's shard/meta persistence is
 *  exercised without a filesystem. */
private class MemoryMirrorFiles : WikiMirrorFiles {
    val files = mutableMapOf<String, String>()

    override fun read(name: String): String? = files[name]

    override fun write(name: String, content: String) {
        files[name] = content
    }

    override fun delete(name: String) {
        files.remove(name)
    }
}

private fun page(
    path: String,
    title: String = path.substringAfterLast('/'),
    body: String = "본문 $path",
    summary: String = "",
    updated: String = "2026-07-17",
    tags: List<String> = emptyList(),
) = WikiPage(
    path = path,
    title = title,
    summary = summary,
    category = wikiMirrorCategoryOf(path),
    tags = tags,
    updated = updated,
    body = body,
)

class WikiMirrorStoreTest {
    private val owner = "gw#token-a"

    private fun store(files: WikiMirrorFiles, owner: () -> String = { this.owner }) = WikiMirrorStore(files, owner)

    @Test
    fun replaceAllServesPagesListingsAndCategories() = runTest {
        val s = store(MemoryMirrorFiles())
        s.replaceAll(
            listOf(
                page("프로젝트/alpha.md", updated = "2026-07-01"),
                page("프로젝트/beta.md", updated = "2026-07-10"),
                page("사람/kim.md"),
            ),
            nowMs = 1_000,
        )

        assertEquals(3, s.pageCount())
        assertEquals(1_000, s.syncedAtMs())
        assertEquals("본문 사람/kim.md", s.get("사람/kim.md")?.body)

        // Category listing follows the list_in_category contract: newest first.
        val proj = s.listCategory("프로젝트")
        assertEquals(listOf("프로젝트/beta.md", "프로젝트/alpha.md"), proj.map { it.path })

        val cats = s.categories()!!
        assertEquals(3, cats.totalPages)
        assertEquals(setOf("프로젝트", "사람"), cats.categories.map { it.name }.toSet())
    }

    @Test
    fun persistsAcrossStoreInstances() = runTest {
        val files = MemoryMirrorFiles()
        store(files).replaceAll(listOf(page("a/one.md"), page("b/two.md")), nowMs = 42)

        val reopened = store(files)
        assertEquals(2, reopened.pageCount())
        assertEquals(42, reopened.syncedAtMs())
        assertEquals("본문 a/one.md", reopened.get("a/one.md")?.body)
    }

    @Test
    fun upsertAndRemoveSurviveReopen() = runTest {
        val files = MemoryMirrorFiles()
        val s = store(files)
        s.replaceAll(listOf(page("a/one.md")), nowMs = 1)
        s.upsert(page("a/two.md", body = "fresh"))
        s.remove("a/one.md")

        val reopened = store(files)
        assertNull(reopened.get("a/one.md"))
        assertEquals("fresh", reopened.get("a/two.md")?.body)
    }

    @Test
    fun foreignOwnerMirrorIsWipedNotServed() = runTest {
        val files = MemoryMirrorFiles()
        store(files) { "gw#token-a" }.replaceAll(listOf(page("a/secret.md")), nowMs = 7)

        val other = store(files) { "gw#token-B" }
        assertEquals(0, other.pageCount())
        assertEquals(0, other.syncedAtMs())
        assertTrue(files.files.isEmpty(), "foreign mirror files must be deleted, got ${files.files.keys}")
    }

    @Test
    fun searchRanksTitleHitsAboveBodyHits() = runTest {
        val s = store(MemoryMirrorFiles())
        s.replaceAll(
            listOf(
                page("a/notes.md", title = "회의록", body = "태양광 모듈 가격 논의"),
                page("b/solar.md", title = "태양광 사업", body = "개요"),
            ),
            nowMs = 1,
        )

        val hits = s.search("태양광")
        assertEquals(listOf("b/solar.md", "a/notes.md"), hits.map { it.path })
        // Multi-token: every token must match somewhere.
        assertEquals(listOf("a/notes.md"), s.search("태양광 가격").map { it.path })
        assertTrue(s.search("존재하지않는어휘").isEmpty())
    }

    @Test
    fun inMemoryMirrorRejectsForeignOwnerOnRead() = runTest {
        val files = MemoryMirrorFiles()
        var currentOwner = "gw#token-a"
        val s = store(files) { currentOwner }
        s.replaceAll(listOf(page("a/secret.md")), nowMs = 1)

        // Credential switch before async clear() finishes — must not serve A's page.
        currentOwner = "gw#token-b"
        assertNull(s.get("a/secret.md"))
        assertEquals(0, s.pageCount())
        assertNull(s.search("secret").ifEmpty { null })
    }

    @Test
    fun clearDropsMemoryAndDisk() = runTest {
        val files = MemoryMirrorFiles()
        val s = store(files)
        s.replaceAll(listOf(page("a/one.md")), nowMs = 1)
        s.clear()

        assertEquals(0, s.pageCount())
        assertTrue(files.files.isEmpty())
    }
}
