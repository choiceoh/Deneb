package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SmsStoreTest {

    private data class Fixture(val settings: AppSettings, val store: SmsStore)

    private fun fixture(): Fixture {
        val settings = AppSettings(MapSettings())
        return Fixture(settings, SmsStore(settings))
    }

    private fun message(id: Long, body: String = "body-$id") = SmsMessage(
        id = id,
        address = "+821000000000",
        date = id * 1_000,
        preview = body.take(10),
        body = body,
    )

    @Test
    fun missingAndMalformedSyncStateUseDefaults() {
        val f = fixture()
        assertEquals(SmsSyncState(), f.store.getSyncState())

        for (raw in listOf("broken", "{}[]", "[1]")) {
            f.settings.setSmsSyncStateJson(raw)
            assertEquals(SmsSyncState(), f.store.getSyncState(), raw)
        }
    }

    @Test
    fun syncStateRoundTripsAcrossStoreRecreation() = runTest {
        val f = fixture()
        val state = SmsSyncState(
            lastSeenId = 77,
            lastSyncEpochMs = 1_000,
            lastAttemptEpochMs = 1_100,
            unreadCount = 3,
            lastError = "permission",
        )

        f.store.updateSyncState(state)

        assertEquals(state, f.store.getSyncState())
        assertEquals(state, SmsStore(f.settings).getSyncState())
    }

    @Test
    fun pendingMessagesPreserveFifoOrderAcrossAdds() = runTest {
        val f = fixture()

        f.store.addPending(listOf(message(1), message(2)))
        f.store.addPending(listOf(message(3)))

        assertEquals(listOf(1L, 2L, 3L), f.store.getPending().map { it.id })
        assertEquals(listOf(1L, 2L, 3L), SmsStore(f.settings).getPending().map { it.id })
    }

    @Test
    fun removalUsesStableSmsIdAndRemovesAllDuplicates() = runTest {
        val f = fixture()
        f.store.addPending(listOf(message(1, "old"), message(1, "duplicate"), message(2)))

        f.store.removePending(listOf(message(1, "different payload")))

        assertEquals(listOf(message(2)), f.store.getPending())
    }

    @Test
    fun removingUnknownMessageLeavesExistingQueue() = runTest {
        val f = fixture()
        val existing = listOf(message(1), message(2))
        f.store.addPending(existing)

        f.store.removePending(listOf(message(99)))

        assertEquals(existing, f.store.getPending())
    }

    @Test
    fun pendingQueueCapsAtNewestHundredMessages() = runTest {
        val f = fixture()

        f.store.addPending((1L..120L).map(::message))

        assertEquals(100, f.store.getPending().size)
        assertEquals((21L..120L).toList(), f.store.getPending().map { it.id })
    }

    @Test
    fun malformedPendingStorageReadsEmptyAndHealsOnAdd() = runTest {
        val f = fixture()
        f.settings.setSmsPendingJson("not-json")

        assertEquals(emptyList(), f.store.getPending())
        f.store.addPending(listOf(message(5)))

        assertEquals(listOf(message(5)), f.store.getPending())
        assertTrue(f.settings.getSmsPendingJson().startsWith("["))
    }

    @Test
    fun emptyMutationsLeavePendingQueueEmpty() = runTest {
        val f = fixture()

        f.store.addPending(emptyList())
        f.store.removePending(emptyList())

        assertEquals(emptyList(), f.store.getPending())
        assertEquals("", f.settings.getSmsPendingJson())
    }
}
