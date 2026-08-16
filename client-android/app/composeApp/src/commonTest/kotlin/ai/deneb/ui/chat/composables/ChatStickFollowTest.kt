package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ChatStickFollowTest {

    @Test
    fun imeShrinkWhilePinnedFollowsTheLostHeight() {
        assertEquals(
            320,
            chatStickFollowScrollPx(
                previousViewportHeight = 800,
                viewportHeight = 480,
                previousLastKey = 4,
                lastKey = 4,
                previousLastBottom = 800,
                lastBottom = 800,
                stickToBottom = true,
            ),
        )
    }

    @Test
    fun lastRowGrowthWhilePinnedFollowsTheNewTokens() {
        assertEquals(
            36,
            chatStickFollowScrollPx(
                previousViewportHeight = 800,
                viewportHeight = 800,
                previousLastKey = 4,
                lastKey = 4,
                previousLastBottom = 800,
                lastBottom = 836,
                stickToBottom = true,
            ),
        )
    }

    @Test
    fun waitingChipPushedDownByAGrowingAnswerStillFollows() {
        // Last visible row is the waiting chip (stable height); the streaming
        // answer above it grows and pushes the chip's bottom down.
        assertEquals(
            48,
            chatStickFollowScrollPx(
                previousViewportHeight = 800,
                viewportHeight = 800,
                previousLastKey = 5,
                lastKey = 5,
                previousLastBottom = 790,
                lastBottom = 838,
                stickToBottom = true,
            ),
        )
    }

    @Test
    fun imeAndGrowthInTheSameFrameAdd() {
        assertEquals(
            356,
            chatStickFollowScrollPx(
                previousViewportHeight = 800,
                viewportHeight = 480,
                previousLastKey = 4,
                lastKey = 4,
                previousLastBottom = 800,
                lastBottom = 836,
                stickToBottom = true,
            ),
        )
    }

    @Test
    fun scrolledUpDoesNotFollow() {
        assertEquals(
            0,
            chatStickFollowScrollPx(
                previousViewportHeight = 800,
                viewportHeight = 480,
                previousLastKey = 4,
                lastKey = 4,
                previousLastBottom = 800,
                lastBottom = 900,
                stickToBottom = false,
            ),
        )
    }

    @Test
    fun firstLayoutAndNewLastRowDoNotJump() {
        assertEquals(
            0,
            chatStickFollowScrollPx(
                previousViewportHeight = 0,
                viewportHeight = 800,
                previousLastKey = -1,
                lastKey = 4,
                previousLastBottom = 0,
                lastBottom = 800,
                stickToBottom = true,
            ),
        )
        assertEquals(
            0,
            chatStickFollowScrollPx(
                previousViewportHeight = 800,
                viewportHeight = 800,
                previousLastKey = 3,
                lastKey = 4,
                previousLastBottom = 720,
                lastBottom = 800,
                stickToBottom = true,
            ),
        )
    }

    @Test
    fun keyboardCloseDoesNotScrollFromThisHelper() {
        assertEquals(
            0,
            chatStickFollowScrollPx(
                previousViewportHeight = 480,
                viewportHeight = 800,
                previousLastKey = 4,
                lastKey = 4,
                previousLastBottom = 480,
                lastBottom = 480,
                stickToBottom = true,
            ),
        )
    }

    @Test
    fun keepPinnedAcrossImeAndStreamingGrowth() {
        assertTrue(chatStickKeepPinned(wasPinned = true, nearBottom = false, viewportShrunk = true, contentGrew = false))
        assertTrue(chatStickKeepPinned(wasPinned = true, nearBottom = false, viewportShrunk = false, contentGrew = true))
        assertFalse(chatStickKeepPinned(wasPinned = true, nearBottom = false, viewportShrunk = false, contentGrew = false))
        assertTrue(chatStickKeepPinned(wasPinned = false, nearBottom = true, viewportShrunk = false, contentGrew = false))
    }

    @Test
    fun userDragDetachesEvenWhileTokensGrow() {
        assertFalse(
            chatStickKeepPinned(
                wasPinned = true,
                nearBottom = false,
                viewportShrunk = false,
                contentGrew = true,
                userDragging = true,
            ),
        )
    }

    @Test
    fun waitingChipAppendKeepsThePin() {
        assertTrue(
            chatStickKeepPinned(
                wasPinned = true,
                nearBottom = false,
                viewportShrunk = false,
                contentGrew = false,
                listGrew = true,
            ),
        )
    }

    @Test
    fun jumpToReportDoesNotKeepPinJustBecauseLastVisibleChanged() {
        // total unchanged (not listGrew); user/programmatic navigation away.
        assertFalse(
            chatStickKeepPinned(
                wasPinned = true,
                nearBottom = false,
                viewportShrunk = false,
                contentGrew = false,
                listGrew = false,
            ),
        )
    }

    @Test
    fun snapWhenPinnedButLastRowNotComposedOrListGrewOrImeClosed() {
        assertTrue(chatStickNeedsSnap(stickToBottom = true, lastIndex = 3, total = 6, listGrew = false, viewportGrew = false))
        assertTrue(chatStickNeedsSnap(stickToBottom = true, lastIndex = 5, total = 6, listGrew = true, viewportGrew = false))
        assertTrue(chatStickNeedsSnap(stickToBottom = true, lastIndex = 5, total = 6, listGrew = false, viewportGrew = true))
        assertFalse(chatStickNeedsSnap(stickToBottom = true, lastIndex = 5, total = 6, listGrew = false, viewportGrew = false))
        assertFalse(chatStickNeedsSnap(stickToBottom = false, lastIndex = 3, total = 6, listGrew = true, viewportGrew = true))
    }
}
