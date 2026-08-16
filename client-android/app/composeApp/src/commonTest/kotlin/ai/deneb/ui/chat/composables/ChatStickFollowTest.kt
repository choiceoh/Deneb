package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals

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
                previousLastSize = 120,
                lastSize = 120,
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
                previousLastSize = 120,
                lastSize = 156,
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
                previousLastSize = 120,
                lastSize = 156,
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
                previousLastSize = 120,
                lastSize = 200,
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
                previousLastSize = 0,
                lastSize = 120,
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
                previousLastSize = 80,
                lastSize = 120,
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
                previousLastSize = 120,
                lastSize = 120,
                stickToBottom = true,
            ),
        )
    }
}
