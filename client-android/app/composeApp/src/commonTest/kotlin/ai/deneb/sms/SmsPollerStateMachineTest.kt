package ai.deneb.sms

import ai.deneb.data.AppSettings
import ai.deneb.data.SmsMessage
import ai.deneb.data.SmsStore
import ai.deneb.data.SmsSyncState
import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlin.time.Clock

class SmsPollerStateMachineTest {

    private class FakeReader : SmsInboxReader {
        var supported = true
        var permission = true
        var maxInboxId = 0L
        var messages = emptyList<SmsMessage>()
        var supportedFailure: Throwable? = null
        var permissionFailure: Throwable? = null
        var maxFailure: Throwable? = null
        var readFailure: Throwable? = null
        var readGate: CompletableDeferred<Unit>? = null
        val calls = mutableListOf<String>()
        val readArgs = mutableListOf<Pair<Long, Int>>()
        var activeReads = 0
        var maxActiveReads = 0

        override fun isSupported(): Boolean {
            calls += "supported"
            supportedFailure?.let { throw it }
            return supported
        }

        override fun hasPermission(): Boolean {
            calls += "permission"
            permissionFailure?.let { throw it }
            return permission
        }

        override suspend fun readInboxSince(lastSeenId: Long, limit: Int): List<SmsMessage> {
            calls += "read"
            readArgs += lastSeenId to limit
            activeReads++
            maxActiveReads = maxOf(maxActiveReads, activeReads)
            try {
                readGate?.await()
                readFailure?.let { throw it }
                return messages
            } finally {
                activeReads--
            }
        }

        override suspend fun currentMaxInboxId(): Long {
            calls += "max"
            maxFailure?.let { throw it }
            return maxInboxId
        }
    }

    private data class Fixture(
        val settings: AppSettings,
        val store: SmsStore,
        val reader: FakeReader,
        val poller: SmsPoller,
    )

    private fun fixture(reader: FakeReader = FakeReader()): Fixture {
        val settings = AppSettings(MapSettings())
        val store = SmsStore(settings)
        return Fixture(settings, store, reader, SmsPoller(store, reader))
    }

    private fun message(id: Long, read: Boolean = false, body: String = "body-$id") = SmsMessage(
        id = id,
        address = "+8210$id",
        date = id * 1000,
        preview = body.take(10),
        body = body,
        read = read,
    )

    private suspend fun markInitialized(store: SmsStore, lastSeenId: Long = 10L, error: String? = null) {
        store.updateSyncState(
            SmsSyncState(
                lastSeenId = lastSeenId,
                lastSyncEpochMs = 1L,
                lastAttemptEpochMs = 1L,
                lastError = error,
            ),
        )
    }

    @Test
    fun unsupportedPlatformReturnsWithoutTouchingState() = runTest {
        val f = fixture()
        f.reader.supported = false

        f.poller.poll()

        assertEquals(listOf("supported"), f.reader.calls)
        assertEquals(SmsSyncState(), f.store.getSyncState())
    }

    @Test
    fun unsupportedCheckFailurePropagatesBecauseNoPollingAttemptStarted() = runTest {
        val f = fixture()
        f.reader.supportedFailure = IllegalStateException("platform probe failed")

        val failure = assertFailsWith<IllegalStateException> { f.poller.poll() }

        assertEquals("platform probe failed", failure.message)
        assertEquals(SmsSyncState(), f.store.getSyncState())
    }

    @Test
    fun deniedPermissionRecordsAttemptButPreservesSyncProgress() = runTest {
        val f = fixture()
        f.reader.permission = false
        val previous = SmsSyncState(lastSeenId = 17, lastSyncEpochMs = 9, unreadCount = 4)
        f.store.updateSyncState(previous)
        val before = Clock.System.now().toEpochMilliseconds()

        f.poller.poll()
        val state = f.store.getSyncState()

        assertEquals(17L, state.lastSeenId)
        assertEquals(9L, state.lastSyncEpochMs)
        assertEquals(4, state.unreadCount)
        assertEquals("Permission not granted", state.lastError)
        assertTrue(state.lastAttemptEpochMs >= before)
        assertEquals(listOf("supported", "permission"), f.reader.calls)
    }

    @Test
    fun firstAuthorizedPollSeedsCursorWithoutReadingHistory() = runTest {
        val f = fixture()
        f.reader.maxInboxId = 321L

        f.poller.poll()
        val state = f.store.getSyncState()

        assertEquals(321L, state.lastSeenId)
        assertTrue(state.lastSyncEpochMs > 0)
        assertEquals(state.lastSyncEpochMs, state.lastAttemptEpochMs)
        assertNull(state.lastError)
        assertEquals(listOf("supported", "permission", "max"), f.reader.calls)
        assertEquals(emptyList(), f.store.getPending())
    }

    @Test
    fun firstPollAgainstEmptyInboxStillMarksInitializationComplete() = runTest {
        val f = fixture()
        f.reader.maxInboxId = 0L

        f.poller.poll()

        assertEquals(0L, f.store.getSyncState().lastSeenId)
        assertTrue(f.store.getSyncState().lastSyncEpochMs > 0)
    }

    @Test
    fun firstMessageAfterInitiallyEmptyInboxIsNotSilentlySeededAway() = runTest {
        val f = fixture()
        f.reader.maxInboxId = 0L
        f.poller.poll()
        f.reader.calls.clear()
        f.reader.messages = listOf(message(1))

        f.poller.poll()

        assertEquals(listOf(1L), f.store.getPending().map { it.id })
        assertEquals(listOf("supported", "permission", "read"), f.reader.calls)
        assertEquals(1L, f.store.getSyncState().lastSeenId)
    }

    @Test
    fun permissionDenialBeforeGrantStillSeedsExistingHistory() = runTest {
        val f = fixture()
        f.reader.permission = false
        f.poller.poll()
        f.reader.permission = true
        f.reader.maxInboxId = 50
        f.reader.calls.clear()

        f.poller.poll()

        assertEquals(50L, f.store.getSyncState().lastSeenId)
        assertEquals(listOf("supported", "permission", "max"), f.reader.calls)
    }

    @Test
    fun initializedZeroCursorReadsInsteadOfReseeding() = runTest {
        val f = fixture()
        markInitialized(f.store, lastSeenId = 0)
        f.reader.maxInboxId = 999
        f.reader.messages = listOf(message(1))

        f.poller.poll()

        assertEquals(listOf(0L to SmsPoller.MAX_FETCH_PER_POLL), f.reader.readArgs)
        assertFalse("max" in f.reader.calls)
        assertEquals(1L, f.store.getSyncState().lastSeenId)
    }

    @Test
    fun currentMaxFailureRecordsErrorWithoutMarkingInitialized() = runTest {
        val f = fixture()
        f.reader.maxFailure = IllegalStateException("max failed")

        f.poller.poll()
        val state = f.store.getSyncState()

        assertEquals(0L, state.lastSyncEpochMs)
        assertEquals(0L, state.lastSeenId)
        assertEquals("max failed", state.lastError)
        assertTrue(state.lastAttemptEpochMs > 0)
    }

    @Test
    fun cancellationDuringInitialSeedPropagatesWithoutErrorState() = runTest {
        val f = fixture()
        f.reader.maxFailure = CancellationException("cancel max")

        assertFailsWith<CancellationException> { f.poller.poll() }

        assertEquals(SmsSyncState(), f.store.getSyncState())
    }

    @Test
    fun normalPollPassesCursorAndHardLimitToReader() = runTest {
        val f = fixture()
        markInitialized(f.store, 77)

        f.poller.poll()

        assertEquals(listOf(77L to 50), f.reader.readArgs)
    }

    @Test
    fun emptyNormalPollAdvancesSyncTimesClearsErrorAndKeepsCursor() = runTest {
        val f = fixture()
        markInitialized(f.store, 77, error = "old")
        val before = Clock.System.now().toEpochMilliseconds()

        f.poller.poll()
        val after = Clock.System.now().toEpochMilliseconds()
        val state = f.store.getSyncState()

        assertEquals(77L, state.lastSeenId)
        assertTrue(state.lastSyncEpochMs in before..after)
        assertEquals(state.lastSyncEpochMs, state.lastAttemptEpochMs)
        assertEquals(0, state.unreadCount)
        assertNull(state.lastError)
    }

    @Test
    fun newMessagesAreQueuedAndCursorAdvancesToMaximumId() = runTest {
        val f = fixture()
        markInitialized(f.store, 10)
        f.reader.messages = listOf(message(11), message(13), message(12))

        f.poller.poll()

        assertEquals(listOf(11L, 13L, 12L), f.store.getPending().map { it.id })
        assertEquals(13L, f.store.getSyncState().lastSeenId)
    }

    @Test
    fun unreadCountReflectsOnlyEligibleUniqueCurrentBatch() = runTest {
        val f = fixture()
        markInitialized(f.store, 10)
        f.reader.messages = listOf(
            message(9, read = false),
            message(11, read = false),
            message(11, read = true),
            message(12, read = true),
            message(13, read = false),
        )

        f.poller.poll()

        assertEquals(2, f.store.getSyncState().unreadCount)
        assertEquals(listOf(11L, 12L, 13L), f.store.getPending().map { it.id })
    }

    @Test
    fun allReadBatchStoresRowsButSetsUnreadCountZero() = runTest {
        val f = fixture()
        markInitialized(f.store, 1)
        f.reader.messages = listOf(message(2, read = true), message(3, read = true))

        f.poller.poll()

        assertEquals(2, f.store.getPending().size)
        assertEquals(0, f.store.getSyncState().unreadCount)
    }

    @Test
    fun readerRowsAtOrBelowCursorCannotRegressState() = runTest {
        val f = fixture()
        markInitialized(f.store, 100)
        f.reader.messages = listOf(message(Long.MIN_VALUE), message(-1), message(0), message(99), message(100))

        f.poller.poll()

        assertEquals(emptyList(), f.store.getPending())
        assertEquals(100L, f.store.getSyncState().lastSeenId)
    }

    @Test
    fun duplicateReaderRowsAreCollapsedByIdBeforeQueueing() = runTest {
        val f = fixture()
        markInitialized(f.store, 1)
        f.reader.messages = listOf(message(2, body = "first"), message(2, body = "second"))

        f.poller.poll()

        assertEquals(1, f.store.getPending().size)
        assertEquals("first", f.store.getPending().single().body)
    }

    @Test
    fun maliciousReaderCannotBypassFiftyRowCap() = runTest {
        val f = fixture()
        markInitialized(f.store, 0)
        f.reader.messages = (1L..80L).map(::message)

        f.poller.poll()

        assertEquals(50, f.store.getPending().size)
        assertEquals(50L, f.store.getSyncState().lastSeenId)
    }

    @Test
    fun existingPendingRowsRemainDeduplicatedAcrossPolls() = runTest {
        val f = fixture()
        markInitialized(f.store, 1)
        f.store.addPending(listOf(message(2, body = "existing")))
        f.reader.messages = listOf(message(2, body = "new duplicate"), message(3))

        f.poller.poll()

        assertEquals(listOf(2L, 3L), f.store.getPending().map { it.id })
        assertEquals("existing", f.store.getPending().first().body)
    }

    @Test
    fun permissionProbeFailureIsRecordedAsAttemptFailure() = runTest {
        val f = fixture()
        markInitialized(f.store, 5)
        f.reader.permissionFailure = IllegalStateException("permission probe failed")

        f.poller.poll()

        assertEquals("permission probe failed", f.store.getSyncState().lastError)
        assertEquals(5L, f.store.getSyncState().lastSeenId)
    }

    @Test
    fun readFailurePreservesPriorSyncProgress() = runTest {
        val f = fixture()
        val previous = SmsSyncState(lastSeenId = 8, lastSyncEpochMs = 7, lastAttemptEpochMs = 6, unreadCount = 3)
        f.store.updateSyncState(previous)
        f.reader.readFailure = IllegalStateException("read failed")

        f.poller.poll()
        val state = f.store.getSyncState()

        assertEquals(8L, state.lastSeenId)
        assertEquals(7L, state.lastSyncEpochMs)
        assertEquals(3, state.unreadCount)
        assertEquals("read failed", state.lastError)
        assertTrue(state.lastAttemptEpochMs > 6)
    }

    @Test
    fun exceptionWithoutMessageUsesClassName() = runTest {
        val f = fixture()
        markInitialized(f.store)
        f.reader.readFailure = Exception()

        f.poller.poll()

        assertEquals("Exception", f.store.getSyncState().lastError)
    }

    @Test
    fun cancellationDuringReadPropagatesWithoutChangingState() = runTest {
        val f = fixture()
        val previous = SmsSyncState(lastSeenId = 9, lastSyncEpochMs = 8, lastAttemptEpochMs = 7)
        f.store.updateSyncState(previous)
        f.reader.readFailure = CancellationException("cancel read")

        assertFailsWith<CancellationException> { f.poller.poll() }

        assertEquals(previous, f.store.getSyncState())
        assertEquals(emptyList(), f.store.getPending())
    }

    @Test
    fun concurrentPollsNeverOverlapReaderCalls() = runTest {
        val f = fixture()
        markInitialized(f.store, 1)
        f.reader.readGate = CompletableDeferred()

        val first = async { f.poller.poll() }
        val second = async { f.poller.poll() }
        testScheduler.runCurrent()

        assertEquals(1, f.reader.activeReads)
        assertEquals(1, f.reader.readArgs.size)

        f.reader.readGate?.complete(Unit)
        first.await()
        second.await()
        assertEquals(1, f.reader.maxActiveReads)
        assertEquals(2, f.reader.readArgs.size)
    }

    @Test
    fun cancellingPollQueuedOnMutexLeavesActivePollHealthy() = runTest {
        val f = fixture()
        markInitialized(f.store, 1)
        f.reader.readGate = CompletableDeferred()
        val first = async { f.poller.poll() }
        val second = async { f.poller.poll() }
        testScheduler.runCurrent()

        second.cancel()
        f.reader.readGate?.complete(Unit)
        first.await()

        assertTrue(second.isCancelled)
        assertEquals(1, f.reader.readArgs.size)
        assertNull(f.store.getSyncState().lastError)
    }

    @Test
    fun maximumLongMessageIdAdvancesWithoutOverflow() = runTest {
        val f = fixture()
        markInitialized(f.store, Long.MAX_VALUE - 1)
        f.reader.messages = listOf(message(Long.MAX_VALUE))

        f.poller.poll()

        assertEquals(Long.MAX_VALUE, f.store.getSyncState().lastSeenId)
        assertEquals(Long.MAX_VALUE, f.store.getPending().single().id)
    }

    @Test
    fun negativeNonzeroSyncTimestampCountsAsInitializedState() = runTest {
        val f = fixture()
        f.store.updateSyncState(SmsSyncState(lastSeenId = 0, lastSyncEpochMs = -1))

        f.poller.poll()

        assertTrue("read" in f.reader.calls)
        assertFalse("max" in f.reader.calls)
    }
}
