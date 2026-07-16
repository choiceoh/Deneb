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

    private fun pagesPayload(vararg paths: String) = """{"pages":[${paths.joinToString(",") { """{"path":"$it","title":"$it","summary":"","updated":""}""" }}]}"""

    private fun wikiPagePayload(path: String, body: String) = """{"path":"$path","title":"$path","summary":"","category":"","tags":[],"updated":"","body":"$body","code":""}"""

    @Test
    fun categoryPagesCachePerKeyAndReuse() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(pagesPayload("프로젝트/a"))
        f.transport.enqueueRpc(pagesPayload("업무/b"))

        assertEquals(listOf("프로젝트/a"), f.client.fetchCategoryPages("프로젝트")?.map { it.path })
        assertEquals(listOf("프로젝트/a"), f.client.fetchCategoryPages("프로젝트")?.map { it.path })
        assertEquals(listOf("업무/b"), f.client.fetchCategoryPages("업무")?.map { it.path })

        assertEquals(2, f.transport.requests.size)
    }

    @Test
    fun wikiPageSaveInvalidatesPageAndItsCategoryList() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(wikiPagePayload("프로젝트/딜", body = "before"))
        f.transport.enqueueRpc(pagesPayload("프로젝트/딜"))
        f.transport.enqueueRpc("{}") // write_page ack
        f.transport.enqueueRpc(wikiPagePayload("프로젝트/딜", body = "after"))
        f.transport.enqueueRpc(pagesPayload("프로젝트/딜"))

        assertEquals("before", f.client.fetchWikiPage("프로젝트/딜")?.body)
        f.client.fetchCategoryPages("프로젝트")
        f.client.saveWikiPage("프로젝트/딜", body = "after")
        assertEquals("after", f.client.fetchWikiPage("프로젝트/딜")?.body)
        f.client.fetchCategoryPages("프로젝트")

        assertEquals(5, f.transport.requests.size)
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
