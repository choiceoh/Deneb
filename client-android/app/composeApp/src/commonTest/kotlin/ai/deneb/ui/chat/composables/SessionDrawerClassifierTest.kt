package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SessionDrawerClassifierTest {
    @Test
    fun workCardSessionsAreClientPrefixedButFolded() {
        val cases = listOf(
            "client:main:wf-deal-room-42" to true,
            "client:main:wf-" to true,
            "client:main" to false,
            "client:main:abc123" to false,
            "chat:legacy" to false,
            "cron:email-analysis:1" to false,
        )
        for ((id, want) in cases) {
            assertEquals(want, isWorkCardSession(id), id)
            assertFalse(isSystemSession(id) && isWorkCardSession(id), "overlap $id")
        }
    }

    @Test
    fun systemSessionsStayOutOfChatAndCardFolders() {
        assertTrue(isSystemSession("cron:email-analysis:1"))
        assertTrue(isSystemSession("system:boot"))
        assertFalse(isSystemSession("client:main"))
        assertFalse(isSystemSession("client:main:wf-deal"))
        assertFalse(isSystemSession("chat:old"))
    }
}
