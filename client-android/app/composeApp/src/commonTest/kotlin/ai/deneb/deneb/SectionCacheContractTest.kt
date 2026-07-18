package ai.deneb.deneb

import ai.deneb.data.AppSettings
import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.yield
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

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
    fun concurrentDashboardFetchesShareOneInFlightRequest() = runTest {
        val f = gatewayClientFixture()
        val release = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(lanesPayload("one"), gate = release)
        f.transport.enqueueRpc(lanesPayload("duplicate"))

        val first = async { f.client.fetchDashboardLanes() }
        f.transport.awaitRequestCount(1)
        val second = async { f.client.fetchDashboardLanes() }
        yield()

        assertEquals(1, f.transport.requests.size)
        release.complete(Unit)
        assertEquals("one", first.await()?.lanes?.single()?.key)
        assertEquals("one", second.await()?.lanes?.single()?.key)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun invalidationDuringDashboardFetchPreventsStaleRecache() = runTest {
        val f = gatewayClientFixture()
        val release = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(lanesPayload("stale"), gate = release)
        f.transport.enqueueRpc(lanesPayload("fresh"))

        val pending = async { f.client.fetchDashboardLanes() }
        f.transport.awaitRequestCount(1)
        f.client.sectionCaches.dashboard.invalidate()
        release.complete(Unit)

        assertEquals("stale", pending.await()?.lanes?.single()?.key)
        assertEquals("fresh", f.client.fetchDashboardLanes()?.lanes?.single()?.key)
        assertEquals(2, f.transport.requests.size)
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
    fun concurrentCategoryPageFetchesShareOneInFlightRequest() = runTest {
        val f = gatewayClientFixture()
        val release = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(pagesPayload("프로젝트/a"), gate = release)
        f.transport.enqueueRpc(pagesPayload("프로젝트/duplicate"))

        val first = async { f.client.fetchCategoryPages("프로젝트") }
        f.transport.awaitRequestCount(1)
        val second = async { f.client.fetchCategoryPages("프로젝트") }
        yield()

        assertEquals(1, f.transport.requests.size)
        release.complete(Unit)
        assertEquals(first.await(), second.await())
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun differentCategoryPageKeysLoadConcurrently() = runTest {
        val cache = SessionCacheMap<String, String>(SectionCacheTtl)
        val releaseProject = CompletableDeferred<Unit>()
        val projectStarted = CompletableDeferred<Unit>()
        val workStarted = CompletableDeferred<Unit>()

        val project = async {
            cache.getOrLoad("프로젝트") {
                projectStarted.complete(Unit)
                releaseProject.await()
                "프로젝트/a"
            }
        }
        projectStarted.await()
        val work = async {
            cache.getOrLoad("업무") {
                workStarted.complete(Unit)
                "업무/b"
            }
        }
        yield()

        assertTrue(workStarted.isCompleted)
        releaseProject.complete(Unit)
        assertEquals("프로젝트/a", project.await())
        assertEquals("업무/b", work.await())
    }

    @Test
    fun invalidatingOneKeyDoesNotFenceAnotherInFlightLoad() = runTest {
        val cache = SessionCacheMap<String, String>(SectionCacheTtl)
        val projectStarted = CompletableDeferred<Unit>()
        val workStarted = CompletableDeferred<Unit>()
        val releaseProject = CompletableDeferred<Unit>()
        val releaseWork = CompletableDeferred<Unit>()

        val project = async {
            cache.getOrLoad("프로젝트") {
                projectStarted.complete(Unit)
                releaseProject.await()
                "프로젝트/stale"
            }
        }
        val work = async {
            cache.getOrLoad("업무") {
                workStarted.complete(Unit)
                releaseWork.await()
                "업무/fresh"
            }
        }
        projectStarted.await()
        workStarted.await()

        cache.invalidate("프로젝트")
        releaseProject.complete(Unit)
        releaseWork.complete(Unit)

        assertEquals("프로젝트/stale", project.await())
        assertEquals("업무/fresh", work.await())
        assertNull(cache.peek("프로젝트"))
        assertEquals("업무/fresh", cache.peek("업무"))
    }

    @Test
    fun differentKeyCompletionsPersistOneCombinedSnapshot() = runTest {
        val settings = AppSettings(MapSettings())
        val serializer = MapSerializer(String.serializer(), String.serializer())
        val cache = SessionCacheMap(
            ttl = SectionCacheTtl,
            disk = SectionDiskSlot(settings, "parallel-map", serializer) { "owner" },
        )
        val firstStarted = CompletableDeferred<Unit>()
        val secondStarted = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()

        val first = async(Dispatchers.Default) {
            cache.getOrLoad("first") {
                firstStarted.complete(Unit)
                release.await()
                "A"
            }
        }
        val second = async(Dispatchers.Default) {
            cache.getOrLoad("second") {
                secondStarted.complete(Unit)
                release.await()
                "B"
            }
        }
        firstStarted.await()
        secondStarted.await()
        release.complete(Unit)
        assertEquals("A", first.await())
        assertEquals("B", second.await())

        val reopened = SessionCacheMap(
            ttl = SectionCacheTtl,
            disk = SectionDiskSlot(settings, "parallel-map", serializer) { "owner" },
        )
        assertEquals("A", reopened.peek("first"))
        assertEquals("B", reopened.peek("second"))
    }

    @Test
    fun invalidationDuringCategoryFetchPreventsStaleRecache() = runTest {
        val f = gatewayClientFixture()
        val release = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(pagesPayload("프로젝트/stale"), gate = release)
        f.transport.enqueueRpc(pagesPayload("프로젝트/fresh"))

        val pending = async { f.client.fetchCategoryPages("프로젝트") }
        f.transport.awaitRequestCount(1)
        f.client.sectionCaches.categoryPages.invalidate("프로젝트")
        release.complete(Unit)

        assertEquals(listOf("프로젝트/stale"), pending.await()?.map { it.path })
        assertEquals(listOf("프로젝트/fresh"), f.client.fetchCategoryPages("프로젝트")?.map { it.path })
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
    fun diskSnapshotSurvivesRestartAsStalePeekOnly() = runTest {
        val f1 = gatewayClientFixture()
        f1.transport.enqueueRpc(lanesPayload("persisted"))
        f1.client.fetchDashboardLanes()

        // Same settings store = same disk; new client = process restart.
        val f2 = gatewayClientFixture(settings = f1.settings)
        f2.transport.enqueueRpc(lanesPayload("network"))

        // Peek paints the last-known snapshot instantly…
        assertEquals("persisted", f2.client.sectionCaches.dashboard.peek()?.lanes?.single()?.key)
        // …but it is never fresh: the fetch still goes to the network.
        assertEquals("network", f2.client.fetchDashboardLanes()?.lanes?.single()?.key)
        assertEquals(1, f2.transport.requests.size)
    }

    @Test
    fun diskSnapshotRejectsAnotherAccountsOwner() = runTest {
        val f1 = gatewayClientFixture(token = "token-a")
        f1.transport.enqueueRpc(lanesPayload("account-a"))
        f1.client.fetchDashboardLanes()

        val f2 = gatewayClientFixture(token = "token-b", settings = f1.settings)

        assertNull(f2.client.sectionCaches.dashboard.peek())
        assertNull(f2.settings.getCachedSection("dashboard"))
    }

    @Test
    fun corruptDiskSnapshotIsRemovedAfterFirstMiss() = runTest {
        val f = gatewayClientFixture()
        f.settings.putCachedSection("dashboard", "not-json")

        assertNull(f.client.sectionCaches.dashboard.peek())
        assertNull(f.settings.getCachedSection("dashboard"))
    }

    @Test
    fun oversizedSnapshotRemovesOlderUsableValue() {
        val settings = AppSettings(MapSettings())
        val slot = SectionDiskSlot(settings, "large", String.serializer()) { "owner" }
        slot.save("small")
        assertNotNull(settings.getCachedSection("large"))

        slot.save("x".repeat(300_000))

        assertNull(settings.getCachedSection("large"))
    }

    @Test
    fun calendarRangeSnapshotSeedsPeekAcrossRestart() = runTest {
        val f1 = gatewayClientFixture()
        f1.transport.enqueueRpc("""{"events":[{"id":"ev1","summary":"s","start":"2026-07-01T09:00:00+09:00","end":"","allDay":false,"local":false,"category":""}]}""")
        f1.client.fetchCalendarRange("2026-07-01", "2026-08-01")

        val f2 = gatewayClientFixture(settings = f1.settings)

        assertEquals("ev1", f2.client.peekCalendarRange("2026-07-01|2026-08-01")?.single()?.id)
        // Disk-seeded entries are stale by definition — the fetch still goes to the network.
        f2.transport.enqueueRpc("""{"events":[{"id":"network","summary":"s","start":"2026-07-01T09:00:00+09:00","end":"","allDay":false,"local":false,"category":""}]}""")
        assertEquals(
            "network",
            f2.client.fetchCalendarRange("2026-07-01", "2026-08-01")?.single()?.id,
        )
        assertEquals(1, f2.transport.requests.size)
    }

    @Test
    fun wikiChangedSyncEventInvalidatesWikiSnapshots() = runTest {
        val f = gatewayClientFixture()
        // Prime the wiki caches (page body + category list).
        f.transport.enqueueRpc(wikiPagePayload("프로젝트/딜", body = "before"))
        f.transport.enqueueRpc(pagesPayload("프로젝트/딜"))
        f.client.fetchWikiPage("프로젝트/딜")
        f.client.fetchCategoryPages("프로젝트")

        // One sync page carrying the server's wiki.changed, then the empty-feed
        // heal refetch syncNativeState always runs afterwards.
        f.transport.enqueueRpc(
            """{"events":[{"seq":1,"type":"wiki.changed","entityId":"프로젝트/딜","timestampMs":1}],"cursor":1,"hasMore":false}""",
        )
        f.transport.enqueueRpc("""{"items":[]}""")
        f.client.syncNativeState()

        // Both snapshots dropped: the next reads go back to the network.
        f.transport.enqueueRpc(wikiPagePayload("프로젝트/딜", body = "after"))
        f.transport.enqueueRpc(pagesPayload("프로젝트/딜"))
        assertEquals("after", f.client.fetchWikiPage("프로젝트/딜")?.body)
        f.client.fetchCategoryPages("프로젝트")
        // Method-scoped counts (syncNativeState also warms the home caches):
        // 2 page reads + 2 list reads = both snapshots really went back out.
        assertEquals(2, f.transport.requests.count { it.rpcMethod == "miniapp.memory.get_page" })
        assertEquals(2, f.transport.requests.count { it.rpcMethod == "miniapp.memory.list_in_category" })
    }

    @Test
    fun orgChangedSyncEventInvalidatesOrgAndDashboard() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"nodes":[{"id":"a","name":"before"}]}""")
        f.transport.enqueueRpc(lanesPayload("before"))
        f.client.fetchOrg()
        f.client.fetchDashboardLanes()

        f.transport.enqueueRpc(
            """{"events":[{"seq":1,"type":"org.changed","timestampMs":1}],"cursor":1,"hasMore":false}""",
        )
        f.transport.enqueueRpc("""{"items":[]}""")
        f.client.syncNativeState()

        f.transport.enqueueRpc("""{"nodes":[{"id":"a","name":"after"}]}""")
        f.transport.enqueueRpc(lanesPayload("after"))
        assertEquals("after", f.client.fetchOrg()?.nodes?.single()?.name)
        assertEquals("after", f.client.fetchDashboardLanes()?.lanes?.single()?.key)
        assertEquals(2, f.transport.requests.count { it.rpcMethod == "miniapp.org.get" })
        assertEquals(2, f.transport.requests.count { it.rpcMethod == "miniapp.dashboard.lanes" })
    }

    // Enqueue exactly one sync.pull page. The fan-out syncNativeState runs
    // afterwards (feed heal + home-cache warm) hits the harness fallback — the
    // requests are still recorded (which is what the assertions count), and the
    // client tolerates the failed refreshes. No padding: leftover replies would
    // shift the NEXT phase's queue and feed the wrong payload to the wrong call.
    private fun enqueueSyncPage(f: GatewayClientFixture, cursor: Long, events: String) {
        f.transport.enqueueRpc("""{"events":[$events],"cursor":$cursor,"hasMore":false}""")
    }

    @Test
    fun mailChangedSyncEventForcesMailWarmPastThrottle() = runTest {
        val f = gatewayClientFixture()
        fun mailFetches() = f.transport.requests.count { it.rpcMethod == "miniapp.mail.list_recent" }

        // First sync: the home warm runs (cold throttle) — baseline mail fetch.
        enqueueSyncPage(f, cursor = 1, events = "")
        f.client.syncNativeState()
        val baseline = mailFetches()

        // mail.changed clears the warm throttle: the mail list refetches NOW.
        enqueueSyncPage(f, cursor = 2, events = """{"seq":2,"type":"mail.changed","entityId":"k1","timestampMs":1}""")
        f.client.syncNativeState()
        assertEquals(baseline + 1, mailFetches())

        // No event: the throttle holds — no extra mail fetch.
        enqueueSyncPage(f, cursor = 3, events = "")
        f.client.syncNativeState()
        assertEquals(baseline + 1, mailFetches())
    }

    @Test
    fun approvalsChangedSyncEventInvalidatesApprovalsListCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"approvals":[{"docId":"d1","title":"t","canAct":false}],"nextAfterDocId":""}""")
        f.client.fetchApprovals()
        f.client.fetchApprovals() // TTL cache hit — still one network read

        enqueueSyncPage(f, cursor = 1, events = """{"seq":1,"type":"approvals.changed","entityId":"d1","timestampMs":1}""")
        f.client.syncNativeState()

        // Cache dropped: the next read goes back to the network and sees drift.
        f.transport.enqueueRpc("""{"approvals":[{"docId":"d2","title":"t2","canAct":false}],"nextAfterDocId":""}""")
        assertEquals("d2", f.client.fetchApprovals()?.single()?.docId)
        assertEquals(2, f.transport.requests.count { it.rpcMethod == "miniapp.groupware.approvals.list" })
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
