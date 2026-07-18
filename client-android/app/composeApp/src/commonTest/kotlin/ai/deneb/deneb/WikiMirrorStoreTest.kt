package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** In-memory [WikiMirrorFiles] so the store's shard/meta persistence is
 *  exercised without a filesystem. */
private class MemoryMirrorFiles : WikiMirrorFiles {
    val files = mutableMapOf<String, String>()
    var failWriteName: String? = null
    var failWriteNumber: Int? = null
    private var writeCount = 0

    override fun read(name: String): String? = files[name]

    override fun write(name: String, content: String) {
        writeCount++
        if (name == failWriteName || writeCount == failWriteNumber) error("write failed: $name")
        files[name] = content
    }

    override fun delete(name: String) {
        files.remove(name)
    }

    fun resetWriteFailures() {
        failWriteName = null
        failWriteNumber = null
        writeCount = 0
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
    fun failedManifestCommitKeepsPriorGenerationInMemoryAndOnReopen() = runTest {
        val files = MemoryMirrorFiles()
        val s = store(files)
        assertTrue(s.replaceAll(listOf(page("a/old.md")), nowMs = 1))
        val committedManifest = files.files.getValue("meta.json")

        files.resetWriteFailures()
        files.failWriteName = "meta.json"
        assertFalse(s.replaceAll(listOf(page("b/new.md")), nowMs = 2))

        assertEquals("본문 a/old.md", s.get("a/old.md")?.body)
        assertNull(s.get("b/new.md"))
        assertEquals(committedManifest, files.files.getValue("meta.json"))

        val reopened = store(files)
        assertEquals("본문 a/old.md", reopened.get("a/old.md")?.body)
        assertNull(reopened.get("b/new.md"))
        assertEquals(1, reopened.syncedAtMs())
    }

    @Test
    fun failedShardPreparationNeverSwitchesTheCommittedManifest() = runTest {
        val files = MemoryMirrorFiles()
        val s = store(files)
        assertTrue(s.replaceAll(listOf(page("a/old.md")), nowMs = 1))
        val committedManifest = files.files.getValue("meta.json")
        val replacement = pathsInDistinctShards(3).map { page(it) }

        files.resetWriteFailures()
        files.failWriteNumber = 2
        assertFalse(s.replaceAll(replacement, nowMs = 2))

        assertEquals(committedManifest, files.files.getValue("meta.json"))
        assertEquals("본문 a/old.md", store(files).get("a/old.md")?.body)
        replacement.forEach { assertNull(store(files).get(it.path)) }
    }

    @Test
    fun successfulRetryCleansInactiveFilesLeftByFailedPreparation() = runTest {
        val files = MemoryMirrorFiles()
        val s = store(files)
        assertTrue(s.replaceAll(listOf(page("a/old.md")), nowMs = 1))

        files.resetWriteFailures()
        files.failWriteName = "meta.json"
        assertFalse(s.replaceAll(listOf(page("a/new.md")), nowMs = 2))
        assertTrue(files.files.keys.count { it.startsWith("shard-") } >= 2)

        files.resetWriteFailures()
        assertTrue(s.replaceAll(listOf(page("a/new.md")), nowMs = 2))

        assertEquals(1, files.files.keys.count { it.startsWith("shard-") })
        assertNull(store(files).get("a/old.md"))
        assertEquals("본문 a/new.md", store(files).get("a/new.md")?.body)
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
    fun failedPageMutationDoesNotPublishAnUncommittedSnapshot() = runTest {
        val files = MemoryMirrorFiles()
        val s = store(files)
        assertTrue(s.replaceAll(listOf(page("a/one.md")), nowMs = 1))

        files.resetWriteFailures()
        files.failWriteName = "meta.json"
        assertFalse(s.upsert(page("a/two.md")))
        assertFalse(s.remove("a/one.md"))

        assertEquals("본문 a/one.md", s.get("a/one.md")?.body)
        assertNull(s.get("a/two.md"))
        assertEquals("본문 a/one.md", store(files).get("a/one.md")?.body)
    }

    @Test
    fun corruptCommittedShardInvalidatesTheWholeGeneration() = runTest {
        val files = MemoryMirrorFiles()
        assertTrue(store(files).replaceAll(listOf(page("a/one.md"), page("b/two.md")), nowMs = 1))
        val activeShard = files.files.keys.first { it.startsWith("shard-") }
        files.files[activeShard] = "{broken"

        val reopened = store(files)
        assertEquals(0, reopened.pageCount())
        assertEquals(0, reopened.syncedAtMs())
        assertTrue(files.files.isEmpty())
    }

    @Test
    fun legacyFilesMigrateOnTheNextMutation() = runTest {
        val files = MemoryMirrorFiles()
        val legacy = page("legacy/one.md")
        files.files["meta.json"] = """{"owner":"$owner","syncedAtMs":7}"""
        files.files["shard-${testShardOf(legacy.path)}.json"] = Json.encodeToString(mapOf(legacy.path to legacy))
        val s = store(files)

        assertEquals(legacy.body, s.get(legacy.path)?.body)
        assertTrue(s.upsert(page("fresh/two.md")))

        assertTrue(files.files.getValue("meta.json").contains("\"storageVersion\":1"))
        assertTrue(files.files.keys.none { it.matches(Regex("""shard-\d+\.json""")) })
        val reopened = store(files)
        assertEquals(legacy.body, reopened.get(legacy.path)?.body)
        assertEquals("본문 fresh/two.md", reopened.get("fresh/two.md")?.body)
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

private fun testShardOf(path: String): Int = (path.hashCode() and 0x7fffffff) % 16

private fun pathsInDistinctShards(count: Int): List<String> = (0..1_000)
    .map { "project/page-$it.md" }
    .distinctBy(::testShardOf)
    .take(count)
