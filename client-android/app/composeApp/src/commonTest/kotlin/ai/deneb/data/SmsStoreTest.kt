package ai.deneb.data

import com.russhwolf.settings.MapSettings
import com.russhwolf.settings.Settings
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class SmsStoreTest {

    private class SmsSyncSettings(
        private val delegate: Settings = MapSettings(),
    ) : Settings by delegate {
        var failSyncWrites = false
        var syncReads = 0
        var beforeSyncWrite: (() -> Unit)? = null

        override fun getString(key: String, defaultValue: String): String {
            if (key == AppSettings.KEY_SMS_SYNC_STATE) syncReads++
            return delegate.getString(key, defaultValue)
        }

        override fun putString(key: String, value: String) {
            if (key == AppSettings.KEY_SMS_SYNC_STATE) {
                beforeSyncWrite?.invoke()
                if (failSyncWrites) error("sms sync persistence unavailable")
            }
            delegate.putString(key, value)
        }
    }

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
    fun missingAndMalformedSyncStateUseDefaultsAndMalformedStorageIsCleared() {
        val f = fixture()
        assertEquals(SmsSyncState(), f.store.getSyncState())

        for (raw in listOf("broken", "{}[]", "[1]")) {
            f.settings.setSmsSyncStateJson(raw)
            assertEquals(SmsSyncState(), f.store.getSyncState(), raw)
            assertEquals("", f.settings.getSmsSyncStateJson(), raw)
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
    fun syncUpdateReplacesMalformedPayloadWithoutReadingIt() = runTest {
        val raw = SmsSyncSettings()
        val settings = AppSettings(raw)
        settings.setSmsSyncStateJson("broken")
        raw.syncReads = 0
        val store = SmsStore(settings)
        val state = SmsSyncState(lastSeenId = 9, unreadCount = 2)

        store.updateSyncState(state)

        assertEquals(0, raw.syncReads)
        assertEquals(state, store.getSyncState())
    }

    @Test
    fun failedSyncUpdateLeavesPreviouslyCommittedState() = runTest {
        val raw = SmsSyncSettings()
        val settings = AppSettings(raw)
        val stable = SmsSyncState(lastSeenId = 3, unreadCount = 1)
        settings.setSmsSyncStateJson(SharedJson.encodeToString(stable))
        raw.failSyncWrites = true
        val store = SmsStore(settings)

        assertFailsWith<IllegalStateException> {
            store.updateSyncState(SmsSyncState(lastSeenId = 99))
        }

        raw.failSyncWrites = false
        assertEquals(stable, store.getSyncState())
    }

    @Test
    fun readerDuringSyncUpdateCannotClearIncomingValue() = runTest {
        val raw = SmsSyncSettings()
        val settings = AppSettings(raw)
        settings.setSmsSyncStateJson("broken")
        val store = SmsStore(settings)
        val committed = SmsSyncState(lastSeenId = 44)
        var readDuringWrite: SmsSyncState? = null
        raw.beforeSyncWrite = { readDuringWrite = store.getSyncState() }

        store.updateSyncState(committed)

        assertEquals(SmsSyncState(), readDuringWrite)
        assertEquals(committed, store.getSyncState())
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
