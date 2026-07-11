package ai.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ExtensionFunctionsBoundaryTest {

    @Test
    fun humanReadableDateFormatsEpochSecondsInEnglishUtc() {
        assertEquals("01 January 1970", 0L.toHumanReadableDate())
        assertEquals("14 November 2023", 1_700_000_000L.toHumanReadableDate())
        assertEquals("31 December 1969", (-1L).toHumanReadableDate())
    }

    @Test
    fun isoDateUsesUtcAndRejectsNonPositiveProviderSentinels() {
        assertNull(Long.MIN_VALUE.toIsoDate())
        assertNull((-1L).toIsoDate())
        assertNull(0L.toIsoDate())
        assertEquals("1970-01-01", 1L.toIsoDate())
        assertEquals("2023-11-14", 1_700_000_000L.toIsoDate())
    }

    @Test
    fun contextWindowUsesFloorAtThousandAndMillionBoundaries() {
        val cases = linkedMapOf(
            -1L to "-1",
            0L to "0",
            999L to "999",
            1_000L to "1K",
            1_999L to "1K",
            999_999L to "999K",
            1_000_000L to "1M",
            1_999_999L to "1M",
            20_000_000L to "20M",
        )

        cases.forEach { (tokens, expected) -> assertEquals(expected, formatContextWindow(tokens), tokens.toString()) }
    }

    @Test
    fun releaseDateAcceptsYearMonthAndFullDateWithoutValidatingDay() {
        assertEquals("Jan 2026", formatReleaseDate("2026-01"))
        assertEquals("Dec 1999", formatReleaseDate("1999-12-31"))
        assertEquals("Feb 2024", formatReleaseDate("2024-02-99"))
    }

    @Test
    fun releaseDateFallsBackVerbatimForMalformedYearOrMonth() {
        val malformed = listOf("", "2026", "x-01", "2026-x", "2026-00", "2026-13", "-01", " 2026-01")

        malformed.forEach { value -> assertEquals(value, formatReleaseDate(value), value) }
    }

    @Test
    fun fileSizeUsesDecimalGigabytesAndFlooredSmallerUnits() {
        val cases = linkedMapOf(
            -1L to "-1 B",
            0L to "0 B",
            999L to "999 B",
            1_000L to "1 KB",
            1_999L to "1 KB",
            999_999L to "999 KB",
            1_000_000L to "1 MB",
            1_999_999L to "1 MB",
            999_999_999L to "999 MB",
            1_000_000_000L to "1.0 GB",
            1_550_000_000L to "1.5 GB",
            9_999_999_999L to "9.9 GB",
        )

        cases.forEach { (bytes, expected) -> assertEquals(expected, formatFileSize(bytes), bytes.toString()) }
    }

    @Test
    fun veryLargeCountsStayFiniteAndDoNotOverflowFormatting() {
        assertEquals("${Long.MAX_VALUE / 1_000_000}M", formatContextWindow(Long.MAX_VALUE))
        assertTrue(formatFileSize(Long.MAX_VALUE).startsWith("9."))
        assertTrue(formatFileSize(Long.MAX_VALUE).endsWith(" GB"))
    }

    @Test
    fun smartTruncateReturnsShortTextByIdentityContent() {
        for (value in listOf("", "short", "exactly-ten")) {
            assertEquals(value, value.smartTruncate(value.length))
            assertEquals(value, value.smartTruncate(value.length + 100))
        }
    }

    @Test
    fun smartTruncateKeepsBothEndsAndReportsOmittedCount() {
        val source = "A".repeat(100) + "MIDDLE" + "Z".repeat(100)

        val result = source.smartTruncate(100)

        assertTrue(result.startsWith("A".repeat(10)))
        assertTrue(result.endsWith("Z".repeat(10)))
        assertTrue("186 characters truncated" in result)
        assertTrue("MIDDLE" !in result)
    }

    @Test
    fun smartTruncateAtReserveBoundaryCanReturnOnlyMarker() {
        val source = "x".repeat(100)

        assertEquals("\n[... 100 characters truncated ...]\n", source.smartTruncate(80))
    }

    @Test
    fun smartTruncateHandlesTinyAndNonPositiveLimitsWithoutThrowing() {
        val source = "abcdefghij"

        assertEquals("", source.smartTruncate(-1))
        assertEquals("", source.smartTruncate(0))
        assertEquals("a", source.smartTruncate(1))
        assertEquals("abcde", source.smartTruncate(5))
    }

    @Test
    fun smartTruncateOmittedCountMatchesRemovedMiddleForSeveralBudgets() {
        val source = (0 until 300).joinToString("") { (it % 10).toString() }

        for (max in listOf(80, 82, 100, 180, 299)) {
            val keep = (max - 80) / 2
            val omitted = source.length - keep * 2
            val result = source.smartTruncate(max)

            assertTrue(result.startsWith(source.take(keep)), "max=$max")
            assertTrue(result.endsWith(source.takeLast(keep)), "max=$max")
            assertTrue("$omitted characters truncated" in result, "max=$max")
        }
    }

    @Test
    fun smartTruncatePreservesNewlinesAtBothDocumentEnds() {
        val source = "header\n" + "middle".repeat(100) + "\nfooter"
        val result = source.smartTruncate(100)

        assertTrue(result.startsWith(source.take(10)))
        assertTrue(result.endsWith(source.takeLast(10)))
        assertTrue(result.count { it == '\n' } >= 4)
    }
}
