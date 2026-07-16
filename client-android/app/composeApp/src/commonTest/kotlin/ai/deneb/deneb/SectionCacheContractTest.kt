package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * Contract of the section fetch caches (DenebClientSessionCache.kt): a re-entry
 * within the TTL is served from the client cache (no network), force bypasses and
 * replaces, a failure never poisons the cache, and a mutation invalidates.
 * Dashboard and org stand in for the seven converted section fetches — they all
 * share the same SessionCache mechanics.
 */
class SectionCacheContractTest {
    private fun lanesPayload(laneKey: String) = """{"lanes":[{"key":"$laneKey","name":"$laneKey","items":[]}]}"""

    @Test
    fun repeatedDashboardFetchUsesSessionCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(lanesPayload("one"))

        val first = f.client.fetchDashboardLanes()
        val second = f.client.fetchDashboardLanes()

        assertEquals(first, second)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun forcedDashboardFetchBypassesCacheAndReplacesIt() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(lanesPayload("old"))
        f.transport.enqueueRpc(lanesPayload("new"))

        assertEquals("old", f.client.fetchDashboardLanes()?.lanes?.single()?.key)
        assertEquals("new", f.client.fetchDashboardLanes(force = true)?.lanes?.single()?.key)
        assertEquals("new", f.client.fetchDashboardLanes()?.lanes?.single()?.key)

        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun failedDashboardFetchDoesNotPoisonCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad")
        f.transport.enqueueRpc(lanesPayload("recovered"))

        assertNull(f.client.fetchDashboardLanes())
        assertEquals("recovered", f.client.fetchDashboardLanes()?.lanes?.single()?.key)
        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun orgSaveInvalidatesTheOrgCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"nodes":[{"id":"a","name":"before"}]}""")
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueRpc("""{"nodes":[{"id":"a","name":"after"}]}""")

        assertEquals("before", f.client.fetchOrg()?.nodes?.single()?.name)
        f.client.saveOrg(emptyList())
        assertEquals("after", f.client.fetchOrg()?.nodes?.single()?.name)

        assertEquals(3, f.transport.requests.size)
    }
}
