package ai.deneb.deneb

import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.time.Instant

class MailDateFormattingTest {

    private val zone = TimeZone.currentSystemDefault()
    private val today = LocalDate(2026, 7, 11)

    private fun iso(date: LocalDate, hour: Int = 12, minute: Int = 0): String = LocalDateTime(date, LocalTime(hour, minute)).toInstant(zone).toString()

    private fun row(id: String, date: String) = MailMessage(
        id = id,
        from = "sender@example.com",
        subject = id,
        snippet = "preview",
        date = date,
        unread = false,
    )

    @Test
    fun emptyMailProducesNoSections() {
        assertEquals(emptyList(), mailSections(emptyList(), today))
    }

    @Test
    fun sectionsUseTodayYesterdayWeekAndPreviousThresholds() {
        val rows = listOf(
            row("today", iso(today)),
            row("yesterday", iso(today.minus(1, DateTimeUnit.DAY))),
            row("two-days", iso(today.minus(2, DateTimeUnit.DAY))),
            row("six-days", iso(today.minus(6, DateTimeUnit.DAY))),
            row("seven-days", iso(today.minus(7, DateTimeUnit.DAY))),
        )

        val sections = mailSections(rows, today)

        assertEquals(listOf("오늘", "어제", "이번 주", "이전"), sections.map { it.label })
        assertEquals(listOf("today"), sections[0].items.map { it.id })
        assertEquals(listOf("yesterday"), sections[1].items.map { it.id })
        assertEquals(listOf("two-days", "six-days"), sections[2].items.map { it.id })
        assertEquals(listOf("seven-days"), sections[3].items.map { it.id })
    }

    @Test
    fun futureClockSkewIsGroupedWithToday() {
        val future = row("future", iso(today.plus(5, DateTimeUnit.DAY)))

        val sections = mailSections(listOf(future), today)

        assertEquals(listOf("오늘"), sections.map { it.label })
        assertEquals(listOf(future), sections.single().items)
    }

    @Test
    fun malformedDatesFallIntoPreviousWithoutReordering() {
        val first = row("first", "broken")
        val valid = row("today", iso(today))
        val second = row("second", "also-broken")

        val sections = mailSections(listOf(first, valid, second), today)

        assertEquals(listOf("오늘", "이전"), sections.map { it.label })
        assertEquals(listOf("first", "second"), sections.last().items.map { it.id })
    }

    @Test
    fun multipleRowsWithinOneBucketPreserveOriginalNewestFirstOrder() {
        val rows = listOf(
            row("newest", iso(today, 20, 0)),
            row("middle", iso(today, 12, 0)),
            row("oldest", iso(today, 1, 0)),
        )

        val section = mailSections(rows, today).single()

        assertEquals("오늘", section.label)
        assertEquals(listOf("newest", "middle", "oldest"), section.items.map { it.id })
    }

    @Test
    fun sectionOrderIsFixedEvenWhenInputStartsWithOldMail() {
        val rows = listOf(
            row("old", iso(today.minus(30, DateTimeUnit.DAY))),
            row("week", iso(today.minus(3, DateTimeUnit.DAY))),
            row("now", iso(today)),
            row("yday", iso(today.minus(1, DateTimeUnit.DAY))),
        )

        assertEquals(
            listOf("오늘", "어제", "이번 주", "이전"),
            mailSections(rows, today).map { it.label },
        )
    }

    @Test
    fun timeLabelUsesClockForTodayAndFuture() {
        assertEquals("09:05", mailTimeLabel(iso(today, 9, 5), today))
        assertEquals("18:03", mailTimeLabel(iso(today.plus(2, DateTimeUnit.DAY), 18, 3), today))
    }

    @Test
    fun timeLabelUsesYesterdayWeekdayAndMonthDayByAge() {
        val yesterday = today.minus(1, DateTimeUnit.DAY)
        val withinWeek = today.minus(3, DateTimeUnit.DAY)
        val old = today.minus(40, DateTimeUnit.DAY)

        assertEquals("어제", mailTimeLabel(iso(yesterday), today))
        assertEquals(koreanDayOfWeek[withinWeek.dayOfWeek.ordinal], mailTimeLabel(iso(withinWeek), today))
        assertEquals(
            "${old.month.ordinal.plus(1).toString().padStart(2, '0')}-${old.day.toString().padStart(2, '0')}",
            mailTimeLabel(iso(old), today),
        )
    }

    @Test
    fun nullTodayFallsBackToAbsoluteShortDate() {
        val date = iso(today, 7, 4)

        assertEquals(shortDate(date), mailTimeLabel(date, null))
    }

    @Test
    fun malformedTimeLabelUsesRawSubstringFallback() {
        assertEquals("07-11 12:34", mailTimeLabel("2026-07-11T12:34", today))
        assertEquals("short", mailTimeLabel("short", today))
    }

    @Test
    fun shortDateConvertsUtcInstantToSystemZone() {
        val instant = Instant.parse("2000-01-01T16:30:00Z")
        val local = instant.toLocalDateTime(zone)
        val expected = "${(local.month.ordinal + 1).toString().padStart(2, '0')}-" +
            "${local.day.toString().padStart(2, '0')} " +
            "${local.hour.toString().padStart(2, '0')}:${local.minute.toString().padStart(2, '0')}"

        assertEquals(expected, shortDate(instant.toString()))
    }

    @Test
    fun validShortDateAlwaysUsesFixedWidthMonthDayHourMinute() {
        val rendered = shortDate(iso(LocalDate(2000, 2, 3), 4, 5))

        assertTrue(Regex("^02-03 04:05$").matches(rendered), rendered)
    }

    @Test
    fun utcDateNearMidnightUsesLocalCalendarComponentsConsistently() {
        val source = "2000-12-31T23:59:00Z"
        val local = Instant.parse(source).toLocalDateTime(zone)
        val rendered = shortDate(source)

        assertEquals(
            "${(local.month.ordinal + 1).toString().padStart(2, '0')}-" +
                "${local.day.toString().padStart(2, '0')} " +
                "${local.hour.toString().padStart(2, '0')}:${local.minute.toString().padStart(2, '0')}",
            rendered,
        )
    }

    @Test
    fun invalidShortDatePreservesShortRawValue() {
        assertEquals("", shortDate(""))
        assertEquals("bad", shortDate("bad"))
        assertEquals("07-11 12:34", shortDate("2026-07-11T12:34"))
    }
}
