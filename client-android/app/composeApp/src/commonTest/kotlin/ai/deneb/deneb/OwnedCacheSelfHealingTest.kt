package ai.deneb.deneb

import ai.deneb.data.AppSettings
import com.russhwolf.settings.MapSettings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class OwnedCacheSelfHealingTest {

    @Test
    fun absentAndValidCachesDoNotTriggerCleanup() {
        var clears = 0

        assertNull(loadCachedOrClear<String>(null, { error("absent cache must not decode") }, { clears++ }))
        assertEquals("decoded", loadCachedOrClear("raw", { "decoded" }, { clears++ }))
        assertEquals(0, clears)
    }

    @Test
    fun corruptPersistentListCachesAreRemovedAfterFirstMiss() {
        val settings = AppSettings(MapSettings()).apply {
            putCachedMailList("not-json")
            putCachedWorkFeed("not-json")
            putCachedCalendar("not-json")
            putCachedApprovalsList("not-json")
        }

        // Construction reads the home feed and calendar caches; mail and
        // approvals are lazy and exercise the same policy explicitly below.
        val client = gatewayClientFixture(settings = settings).client
        assertNull(client.loadCachedMail())
        assertNull(client.loadCachedApprovals())

        assertNull(settings.getCachedMailList())
        assertNull(settings.getCachedWorkFeed())
        assertNull(settings.getCachedCalendar())
        assertNull(settings.getCachedApprovalsList())
    }
}
