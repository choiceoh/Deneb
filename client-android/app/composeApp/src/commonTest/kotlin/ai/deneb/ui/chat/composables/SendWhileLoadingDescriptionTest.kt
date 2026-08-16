package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals

class SendWhileLoadingDescriptionTest {
    @Test
    fun textOnlyFollowUpIsSteer() {
        assertEquals("끼어들기", sendWhileLoadingDescription(hasFiles = false))
    }

    @Test
    fun filesStayOnTheAfterTurnQueue() {
        assertEquals("보내기 (대기열)", sendWhileLoadingDescription(hasFiles = true))
    }
}
