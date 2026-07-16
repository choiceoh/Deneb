package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalRow
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ApprovalsCacheBoundaryTest {

    private fun row(
        docId: String,
        canAct: Boolean = false,
        title: String = "Title $docId",
    ) = GroupwareApprovalRow(
        docId = docId,
        title = title,
        docNo = "NO-$docId",
        drafter = "기안자",
        date = "2026-07-16",
        status = if (canAct) "대기" else "완료",
        folder = if (canAct) "pending" else "done",
        canAct = canAct,
    )

    @Test
    fun completeApprovalRowRoundTrip() {
        val original = row("d1", canAct = true, title = "한글 결재 🚀")
        assertEquals(
            listOf(original),
            decodeApprovalsCache(encodeApprovalsCache(listOf(original), "owner"), "owner"),
        )
    }

    @Test
    fun persistsOnlyFirstPage() {
        val rows = (1..APPROVALS_PERSIST_PAGE_SIZE + 5).map { row("d$it") }
        val decoded = decodeApprovalsCache(encodeApprovalsCache(rows, "owner"), "owner")!!
        assertEquals(APPROVALS_PERSIST_PAGE_SIZE, decoded.size)
        assertEquals("d1", decoded.first().docId)
        assertEquals("d$APPROVALS_PERSIST_PAGE_SIZE", decoded.last().docId)
    }

    @Test
    fun ownerMismatchAndEmptyAreMisses() {
        val encoded = encodeApprovalsCache(listOf(row("d1")), "owner-a")
        assertNull(decodeApprovalsCache(encoded, "owner-b"))
        assertNull(decodeApprovalsCache(encodeApprovalsCache(emptyList(), "owner"), "owner"))
    }

    @Test
    fun ownerReusesMailFingerprint() {
        val owner = mailCacheOwner("https://gateway.example/", "token")
        assertTrue(owner.startsWith("https://gateway.example#"))
        val roundTrip = decodeApprovalsCache(
            encodeApprovalsCache(listOf(row("d1")), owner),
            owner,
        )
        assertEquals(listOf(row("d1")), roundTrip)
    }
}
