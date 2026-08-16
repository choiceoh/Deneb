package ai.deneb.ui.chat

import kotlin.test.Test
import kotlin.test.assertEquals

class WorkFeedLogCardTest {
    @Test
    fun classifiesBySourceOnly() {
        val cases = listOf(
            "genesis-meta" to true,
            "genesis-ladder" to true,
            "system_log" to true,
            "genesis-evolve-verdict" to true,
            "proactive" to false,
            "mail_report" to false,
            "groupware-approval" to false,
        )
        for ((source, want) in cases) {
            assertEquals(want, isWorkFeedLogCard(source), source)
        }
    }
}
