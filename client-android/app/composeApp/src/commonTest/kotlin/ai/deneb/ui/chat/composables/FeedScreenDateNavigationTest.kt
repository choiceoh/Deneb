package ai.deneb.ui.chat.composables

import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.minus
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FeedScreenDateNavigationTest {
    @Test
    fun emptyTodayCanMoveToPreviousDay() {
        val today = LocalDate(2026, 6, 19)

        val nav = feedDateNavState(
            selectedDate = today,
            today = today,
            loadedDates = emptyList(),
        )

        assertTrue(nav.canGoPrev)
        assertFalse(nav.canGoNext)
    }

    @Test
    fun previousEmptyDayCanMoveBackTowardToday() {
        val today = LocalDate(2026, 6, 19)
        val yesterday = today.minus(1, DateTimeUnit.DAY)

        val nav = feedDateNavState(
            selectedDate = yesterday,
            today = today,
            loadedDates = emptyList(),
        )

        assertTrue(nav.canGoPrev)
        assertTrue(nav.canGoNext)
    }

    @Test
    fun loadedTodayFeedDoesNotBlockPreviousDayBrowse() {
        val today = LocalDate(2026, 6, 19)

        val nav = feedDateNavState(
            selectedDate = today,
            today = today,
            loadedDates = listOf(today),
        )

        assertTrue(nav.canGoPrev)
        assertFalse(nav.canGoNext)
    }

    @Test
    fun loadedOlderFeedExtendsPreviousBound() {
        val today = LocalDate(2026, 6, 19)
        val older = LocalDate(2026, 5, 1)

        val navFromToday = feedDateNavState(
            selectedDate = today,
            today = today,
            loadedDates = listOf(older),
        )
        val navAtOldest = feedDateNavState(
            selectedDate = older,
            today = today,
            loadedDates = listOf(older),
        )

        assertTrue(navFromToday.canGoPrev)
        assertFalse(navAtOldest.canGoPrev)
        assertTrue(navAtOldest.canGoNext)
    }
}
