package ai.deneb.deneb

import kotlinx.datetime.LocalDate
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * The approval date arrives as a STRING passed through verbatim from Amaranth,
 * and its format depends on the folder — measured 2026-07-26, the same RPC
 * returns "2026-07-24 (금)" for folder=total and "26.07.24" for folder=pending.
 * Both have to read, and anything else must yield no age rather than a wrong one:
 * on an approval queue a wrong elapsed count is worse than none.
 */
class ApprovalAgeTest {
    private val today = LocalDate(2026, 7, 26)

    @Test
    fun readsBothFolderFormats() {
        val cases = mapOf(
            "2026-07-24 (금)" to 2, // folder=total
            "2026-07-24" to 2, // same, without the weekday
            "26.07.24" to 2, // folder=pending
            "2026-07-26 (일)" to 0, // today
            "2026-07-16 (목)" to 10, // the ten-day-old document that looked fresh
        )
        val failures = mutableListOf<String>()
        for ((raw, want) in cases) {
            val got = approvalAgeDays(raw, today)
            if (got != want) failures += "$raw → $got, want $want"
        }
        assertEquals(emptyList(), failures)
    }

    @Test
    fun unreadableOrFutureDatesShowNoAge() {
        // A future drafting date means clock skew or a format this parser does not
        // know; either way "-3일 경과" would be nonsense.
        val noAge = listOf("", "   ", "곧", "2026/07/24", "24.07.2026", "2026-07-27 (월)")
        val failures = noAge.filter { approvalAgeDays(it, today) != null }
        assertEquals(emptyList<String>(), failures, "these must yield null")
    }

    @Test
    fun labelsSkipTodayAndNameYesterday() {
        assertNull(approvalAgeLabel(null))
        assertNull(approvalAgeLabel(0), "today needs no elapsed label")
        assertEquals("어제", approvalAgeLabel(1))
        assertEquals("2일 경과", approvalAgeLabel(2))
        assertEquals("10일 경과", approvalAgeLabel(10))
    }

    @Test
    fun staleThresholdMatchesTheAccentRule() {
        // The row paints the accent only at or past this many days AND when the
        // document is the operator's to act on.
        assertEquals(7, APPROVAL_STALE_DAYS)
        assertEquals(6, approvalAgeDays("2026-07-20 (월)", today))
        assertEquals(7, approvalAgeDays("2026-07-19 (일)", today))
    }
}
