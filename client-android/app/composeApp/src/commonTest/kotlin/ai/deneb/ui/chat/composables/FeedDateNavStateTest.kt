package ai.deneb.ui.chat.composables

import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FeedDateNavStateTest {

    private val today = LocalDate(2026, 7, 11)

    @Test
    fun emptyFeedAllowsThirtyOneDayLookbackFromToday() {
        val todayState = feedDateNavState(today, today, emptyList())
        assertTrue(todayState.canGoPrev)
        assertFalse(todayState.canGoNext)

        val fallbackStart = today.minus(31, DateTimeUnit.DAY)
        val startState = feedDateNavState(fallbackStart, today, emptyList())
        assertFalse(startState.canGoPrev)
        assertTrue(startState.canGoNext)
    }

    @Test
    fun anOlderLoadedItemExtendsPreviousNavigation() {
        val older = today.minus(60, DateTimeUnit.DAY)

        val justAfter = feedDateNavState(older.plus(1, DateTimeUnit.DAY), today, listOf(older))
        val atOldest = feedDateNavState(older, today, listOf(older))

        assertTrue(justAfter.canGoPrev)
        assertFalse(atOldest.canGoPrev)
        assertTrue(atOldest.canGoNext)
    }

    @Test
    fun aFutureLoadedItemExtendsNextNavigation() {
        val future = today.plus(10, DateTimeUnit.DAY)

        assertTrue(feedDateNavState(today, today, listOf(future)).canGoNext)
        assertTrue(feedDateNavState(future.minus(1, DateTimeUnit.DAY), today, listOf(future)).canGoNext)
        assertFalse(feedDateNavState(future, today, listOf(future)).canGoNext)
    }

    @Test
    fun loadedBoundsUseMinAndMaxRegardlessOfInputOrder() {
        val oldest = today.minus(50, DateTimeUnit.DAY)
        val newest = today.plus(3, DateTimeUnit.DAY)
        val dates = listOf(today, newest, oldest, today.minus(2, DateTimeUnit.DAY), oldest)

        val middle = feedDateNavState(today, today, dates)
        assertTrue(middle.canGoPrev)
        assertTrue(middle.canGoNext)

        assertFalse(feedDateNavState(oldest, today, dates).canGoPrev)
        assertFalse(feedDateNavState(newest, today, dates).canGoNext)
    }

    @Test
    fun datesInsideFallbackWindowDoNotShrinkDefaultLookback() {
        val loaded = listOf(today.minus(1, DateTimeUnit.DAY), today.minus(10, DateTimeUnit.DAY))
        val fallbackStart = today.minus(31, DateTimeUnit.DAY)

        assertFalse(feedDateNavState(fallbackStart, today, loaded).canGoPrev)
        assertTrue(feedDateNavState(fallbackStart, today, loaded).canGoNext)
    }

    @Test
    fun selectionOutsideKnownBoundsOnlyMovesBackTowardRange() {
        val before = today.minus(100, DateTimeUnit.DAY)
        val after = today.plus(100, DateTimeUnit.DAY)

        val beforeState = feedDateNavState(before, today, emptyList())
        assertFalse(beforeState.canGoPrev)
        assertTrue(beforeState.canGoNext)

        val afterState = feedDateNavState(after, today, emptyList())
        assertTrue(afterState.canGoPrev)
        assertFalse(afterState.canGoNext)
    }

    @Test
    fun futureOnlyLoadedDatesDoNotRemoveFallbackHistoryWindow() {
        val future = today.plus(20, DateTimeUnit.DAY)
        val fallbackStart = today.minus(31, DateTimeUnit.DAY)

        val atStart = feedDateNavState(fallbackStart, today, listOf(future))
        assertFalse(atStart.canGoPrev)
        assertTrue(atStart.canGoNext)

        val middle = feedDateNavState(today, today, listOf(future))
        assertTrue(middle.canGoPrev)
        assertTrue(middle.canGoNext)
    }

    @Test
    fun calendarArithmeticCrossesLeapMonthWithoutOffByOne() {
        val leapToday = LocalDate(2024, 3, 1)
        val fallbackStart = leapToday.minus(31, DateTimeUnit.DAY)

        assertTrue(feedDateNavState(LocalDate(2024, 2, 29), leapToday, emptyList()).canGoPrev)
        assertFalse(feedDateNavState(fallbackStart, leapToday, emptyList()).canGoPrev)
        assertTrue(feedDateNavState(fallbackStart, leapToday, emptyList()).canGoNext)
    }

    // Midnight rollover: the "오늘" view must follow the real today when the app sat in
    // memory past midnight (the reported bug: yesterday's cards under 오늘), but must not
    // drag a date the user manually navigated to.
    @Test
    fun rolloverFollowsWhenSittingOnTheOldToday() {
        assertEquals("2026-07-22", rolledOverSelectedDate("2026-07-21", "2026-07-21", "2026-07-22"))
    }

    @Test
    fun rolloverLeavesAManuallyNavigatedDateAlone() {
        assertEquals("2026-07-15", rolledOverSelectedDate("2026-07-15", "2026-07-21", "2026-07-22"))
    }

    @Test
    fun rolloverIsANoOpWhenTheDayHasNotChanged() {
        assertEquals("2026-07-22", rolledOverSelectedDate("2026-07-22", "2026-07-22", "2026-07-22"))
    }
}
