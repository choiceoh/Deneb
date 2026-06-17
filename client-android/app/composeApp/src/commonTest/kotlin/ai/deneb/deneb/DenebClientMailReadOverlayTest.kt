package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertSame
import kotlin.test.assertTrue

/**
 * The read-overlay that keeps a mail the user has read from re-showing its unread
 * dot after a list refetch. The gateway caches list_recent for 30s and mark_read
 * does not invalidate it, so a phone back-nav (which recomposes the list and
 * re-runs refreshMail) would otherwise replay a stale isUnread=true. These cover
 * the two pure helpers behind that fix; the RPC plumbing is exercised separately.
 */
class DenebClientMailReadOverlayTest {

    private fun row(id: String, unread: Boolean) = MailMessage(
        id = id,
        from = "a@b.com",
        subject = "s",
        snippet = "x",
        date = "2026-06-11T00:00:00Z",
        unread = unread,
    )

    @Test
    fun overlay_clears_the_dot_only_for_read_ids() {
        val out = applyReadOverlay(listOf(row("1", true), row("2", true), row("3", false)), setOf("1"))
        assertFalse(out.first { it.id == "1" }.unread, "read id shows no dot")
        assertTrue(out.first { it.id == "2" }.unread, "an unrelated unread mail keeps its dot")
        assertFalse(out.first { it.id == "3" }.unread, "an already-read mail is untouched")
    }

    @Test
    fun empty_overlay_returns_the_same_instance() {
        val rows = listOf(row("1", true))
        assertSame(rows, applyReadOverlay(rows, emptySet()), "no overlay must not allocate a new list")
    }

    @Test
    fun overlay_survives_a_cache_stale_unread_payload() {
        // Reproduces the bug input: the gateway's 30s list cache replays
        // isUnread=true for a message mark_read already cleared.
        val cachedStale = listOf(row("m1", true))
        assertFalse(
            applyReadOverlay(cachedStale, setOf("m1")).single().unread,
            "a refetch within the cache window must not resurrect the dot",
        )
    }

    @Test
    fun recordReadId_caps_the_set_evicting_the_oldest() {
        val set = LinkedHashSet<String>()
        recordReadId(set, "old", max = 2)
        recordReadId(set, "mid", max = 2)
        recordReadId(set, "new", max = 2)
        assertEquals(setOf("mid", "new"), set, "over cap drops the longest-untouched id")
        assertFalse("old" in set)
    }

    @Test
    fun recordReadId_reread_refreshes_recency() {
        val set = LinkedHashSet<String>()
        recordReadId(set, "a", max = 2)
        recordReadId(set, "b", max = 2)
        recordReadId(set, "a", max = 2) // touch "a" again -> "b" becomes oldest
        recordReadId(set, "c", max = 2) // evicts the oldest
        assertEquals(setOf("a", "c"), set, "recently re-read id survives; the now-oldest is evicted")
        assertFalse("b" in set)
    }

    @Test
    fun mail_cache_is_scoped_to_gateway_owner() {
        val owner = mailCacheOwner("https://srv1.example.test/", "token-a")
        val encoded = encodeMailCache(listOf(row("m1", true)), owner)

        assertEquals("m1", decodeMailCache(encoded, owner)?.single()?.id)
        assertNull(decodeMailCache(encoded, mailCacheOwner("https://srv2.example.test/", "token-a")))
        assertNull(decodeMailCache(encoded, mailCacheOwner("https://srv1.example.test/", "token-b")))
    }

    @Test
    fun mail_cache_rejects_legacy_unscoped_rows() {
        val legacy = """
            [
              {
                "id": "legacy",
                "from": "a@b.com",
                "subject": "old",
                "snippet": "cached",
                "date": "2026-06-11T00:00:00Z",
                "unread": true
              }
            ]
        """.trimIndent()

        assertNull(decodeMailCache(legacy, mailCacheOwner("https://srv1.example.test", "token")))
    }

    @Test
    fun mail_row_native_meta_summarizes_attachment_and_mailbox() {
        assertNull(mailRowNativeMeta(row("plain", false)))

        val attached = row("attached", false).copy(hasAttachment = true, attachmentCount = 2)
        assertEquals("첨부 2", mailRowNativeMeta(attached))

        val archived = row("archived", false).copy(mailbox = "Gmail")
        assertEquals("Gmail 보관함", mailRowNativeMeta(archived))

        val both = row("both", false).copy(hasAttachment = true, attachmentCount = 1, mailbox = "Gmail")
        assertEquals("첨부 · Gmail 보관함", mailRowNativeMeta(both))

        val analyzed = row("analyzed", false).copy(
            workState = MailWorkState(
                analysisStatus = "done",
                analysisQuality = "attention",
                calendarProposalCount = 1,
                todoCount = 2,
            ),
        )
        assertEquals("분석: 확인 · 일정 후보 1 · 할 일 2", mailRowNativeMeta(analyzed))

        val failed = row("failed", false).copy(workState = MailWorkState(analysisStatus = "failed"))
        assertEquals("분석 실패", mailRowNativeMeta(failed))
    }

    @Test
    fun native_status_line_summarizes_archive_state() {
        val line = mailNativeStatusLine(
            MailNativeStatus(
                source = "archive",
                available = true,
                offlineCapable = true,
                mailboxes = listOf(
                    MailNativeMailbox(
                        name = "INBOX",
                        total = 12,
                        unread = 3,
                        locallyRead = 2,
                        locallyArchived = 1,
                        locallyTrashed = 0,
                        latestUid = "55",
                        attachmentCapable = true,
                    ),
                    MailNativeMailbox(
                        name = "Gmail",
                        total = 90,
                        unread = 0,
                        locallyRead = 0,
                        locallyArchived = 0,
                        locallyTrashed = 1,
                        latestUid = "9",
                        attachmentCapable = true,
                    ),
                ),
                overlay = MailNativeOverlay(messages = 4, read = 2, archived = 1, trashed = 1),
                pipeline = MailNativePipeline(analyzed = 5, failed = 1, feedMissing = 2, calendarCandidates = 1),
            ),
        )

        assertEquals("로컬 보관함 · 2개함 102통 · 미읽음 3 · 로컬정리 2 · 분석 5 · 실패 1 · 피드대기 2 · 일정 1 · 오프라인", line)
    }
}
