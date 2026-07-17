package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class WikiMirrorRefreshBoundaryTest {

    private fun page(path: String, body: String = "body") = WikiPage(
        path = path,
        title = path,
        summary = "",
        category = wikiMirrorCategoryOf(path),
        tags = emptyList(),
        updated = "2026-07-17",
        body = body,
    )

    @Test
    fun refreshWikiMirrorFull_emptyPayloadWithPositiveTotalKeepsMirror() = runTest {
        val f = gatewayClientFixture(wikiMirrorFiles = MemoryWikiMirrorFiles())
        f.client.wikiMirror.replaceAll(listOf(page("saved.md", body = "keep")), nowMs = 1)

        f.transport.enqueueRpc("""{"pages":[],"nextCursor":"","hasMore":false,"total":10}""")

        assertFalse(f.client.refreshWikiMirrorFull())
        assertEquals(1, f.client.wikiMirror.pageCount())
        assertEquals("keep", f.client.wikiMirror.get("saved.md")?.body)
    }

    @Test
    fun refreshWikiMirrorFull_incompletePaginationKeepsMirror() = runTest {
        val f = gatewayClientFixture(wikiMirrorFiles = MemoryWikiMirrorFiles())
        f.client.wikiMirror.replaceAll(listOf(page("saved.md")), nowMs = 1)

        f.transport.enqueueRpc(
            """{"pages":[{"path":"a.md","title":"A","body":"a"}],"nextCursor":"a.md","hasMore":true,"total":2}""",
        )
        f.transport.enqueueRpc(
            """{"pages":[],"nextCursor":"a.md","hasMore":true,"total":2}""",
        )

        assertFalse(f.client.refreshWikiMirrorFull())
        assertEquals(1, f.client.wikiMirror.pageCount())
        assertEquals("saved.md", f.client.wikiMirror.get("saved.md")?.path)
    }

    @Test
    fun refreshWikiMirrorFull_completeEmptyWikiReplacesMirror() = runTest {
        val f = gatewayClientFixture(wikiMirrorFiles = MemoryWikiMirrorFiles())
        f.client.wikiMirror.replaceAll(listOf(page("stale.md")), nowMs = 1)

        f.transport.enqueueRpc("""{"pages":[],"nextCursor":"","hasMore":false,"total":0}""")

        assertTrue(f.client.refreshWikiMirrorFull())
        assertEquals(0, f.client.wikiMirror.pageCount())
    }
}
