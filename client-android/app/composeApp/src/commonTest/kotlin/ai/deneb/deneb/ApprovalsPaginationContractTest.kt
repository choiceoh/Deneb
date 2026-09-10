package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals

/** Cached first-page rows and their continuation cursor must describe the same page. */
class ApprovalsPaginationContractTest {
    @BeforeTest
    @AfterTest
    fun resetCache() {
        invalidateApprovalsListCache()
    }

    @Test
    fun reentryAfterAdditionalPageResumesFromFirstPageCursor() = runTest {
        assertReentryRestoresFirstPage(listOf("d081", "d061"))
    }

    @Test
    fun reentryAfterLastPageCanLoadThatPageAgain() = runTest {
        assertReentryRestoresFirstPage(listOf("d081", ""))
    }

    @Test
    fun reentryAfterRowLimitCanLoadBeyondFirstPageAgain() = runTest {
        assertReentryRestoresFirstPage(listOf("d081", "d061", "d041", "d021", "d001"))
    }

    private suspend fun assertReentryRestoresFirstPage(cursors: List<String>) {
        val f = gatewayClientFixture()
        val pages = cursors.indices.map { page -> docIds(100 - page * APPROVALS_PERSIST_PAGE_SIZE) }
        f.transport.enqueueRpc(approvalPage(pages.first(), cursors.first()))
        f.client.fetchApprovals()
        for (page in 1 until pages.size) {
            f.transport.enqueueRpc(approvalPage(pages[page], cursors[page]))
            f.client.loadMoreApprovals()
        }
        assertEquals(pages.flatten(), f.client.denebApprovals.value.map { it.docId })

        // Re-entering the screen restores only page one from the TTL cache.
        assertEquals(pages.first(), f.client.fetchApprovals()?.map { it.docId })
        assertEquals(cursors.first(), f.client.denebApprovalsNextAfter.value)
        assertEquals(pages.size, f.transport.requests.size)

        f.transport.enqueueRpc(approvalPage(pages[1], cursors[1]))
        f.client.loadMoreApprovals()

        assertEquals(cursors.first(), f.transport.requests.last().rpcParams?.get("afterDocId")?.jsonPrimitive?.content)
        assertEquals(pages[0] + pages[1], f.client.denebApprovals.value.map { it.docId })
        assertEquals(pages.size + 1, f.transport.requests.size)
    }

    @Test
    fun smallerPageRefetchesInsteadOfSkippingRowsFromLargerCachedPage() = runTest {
        assertSmallerPageRefetches(cachedCursor = "d001")
    }

    @Test
    fun smallerPageRefetchesInsteadOfTreatingTruncatedCacheAsComplete() = runTest {
        assertSmallerPageRefetches(cachedCursor = "")
    }

    private suspend fun assertSmallerPageRefetches(cachedCursor: String) {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(approvalPage(docIds(100, APPROVALS_MAX_LIMIT), cachedCursor))
        f.client.fetchApprovals(limit = APPROVALS_MAX_LIMIT)

        val firstPage = docIds(100)
        f.transport.enqueueRpc(approvalPage(firstPage, "d081"))
        assertEquals(firstPage, f.client.fetchApprovals()?.map { it.docId })
        assertEquals("d081", f.client.denebApprovalsNextAfter.value)
        assertEquals(2, f.transport.requests.size)
        assertEquals(
            APPROVALS_PERSIST_PAGE_SIZE.toString(),
            f.transport.requests.last().rpcParams?.get("limit")?.jsonPrimitive?.content,
        )

        val secondPage = docIds(80)
        f.transport.enqueueRpc(approvalPage(secondPage, "d061"))
        f.client.loadMoreApprovals()
        assertEquals("d081", f.transport.requests.last().rpcParams?.get("afterDocId")?.jsonPrimitive?.content)
        assertEquals(firstPage + secondPage, f.client.denebApprovals.value.map { it.docId })
    }

    private fun docIds(first: Int, count: Int = APPROVALS_PERSIST_PAGE_SIZE): List<String> = (first downTo first - count + 1).map { "d${it.toString().padStart(3, '0')}" }

    private fun approvalPage(docIds: List<String>, nextAfter: String): String = docIds.joinToString(
        prefix = """{"approvals":[""",
        postfix = """],"nextAfterDocId":"$nextAfter"}""",
    ) { """{"docId":"$it","title":"Title $it"}""" }
}
