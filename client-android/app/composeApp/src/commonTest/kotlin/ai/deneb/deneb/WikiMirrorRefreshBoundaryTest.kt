package ai.deneb.deneb

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.yield
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
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

    @Test
    fun refreshWikiMirrorFull_credSwitchBeforeReplaceAllDoesNotInstallForeignCorpus() = runTest {
        val gate = CompletableDeferred<Unit>()
        val f = gatewayClientFixture(token = "token-a", wikiMirrorFiles = MemoryWikiMirrorFiles())
        f.client.wikiMirror.replaceAll(listOf(page("saved.md", body = "keep")), nowMs = 1)

        f.transport.enqueueRpc(
            """{"pages":[{"path":"secret.md","title":"Secret","body":"from-A"}],"nextCursor":"","hasMore":false,"total":1}""",
            gate = gate,
        )

        val refresh = async { f.client.refreshWikiMirrorFull() }
        yield()
        f.client.onCredentialsChanged("https://gateway.example", "token-b")
        gate.complete(Unit)

        assertFalse(refresh.await())
        assertNull(f.client.wikiMirror.get("secret.md"))
        assertEquals(0, f.client.wikiMirror.pageCount())
    }
}
