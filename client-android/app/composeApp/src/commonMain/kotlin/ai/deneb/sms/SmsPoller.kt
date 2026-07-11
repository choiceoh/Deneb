package ai.deneb.sms

import ai.deneb.data.SmsStore
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

interface SmsInboxReader {
    fun isSupported(): Boolean
    fun hasPermission(): Boolean
    suspend fun readInboxSince(lastSeenId: Long, limit: Int): List<ai.deneb.data.SmsMessage>
    suspend fun currentMaxInboxId(): Long
}

private class PlatformSmsInboxReader(private val delegate: SmsReader) : SmsInboxReader {
    override fun isSupported(): Boolean = delegate.isSupported()
    override fun hasPermission(): Boolean = delegate.hasPermission()
    override suspend fun readInboxSince(lastSeenId: Long, limit: Int) = delegate.readInboxSince(lastSeenId, limit)
    override suspend fun currentMaxInboxId(): Long = delegate.currentMaxInboxId()
}

@OptIn(ExperimentalTime::class)
class SmsPoller(
    private val smsStore: SmsStore,
    private val smsReader: SmsInboxReader,
) {
    constructor(smsStore: SmsStore, smsReader: SmsReader) : this(smsStore, PlatformSmsInboxReader(smsReader))

    private val pollMutex = Mutex()

    suspend fun poll() {
        pollMutex.withLock {
            if (!smsReader.isSupported()) return@withLock
            val syncState = smsStore.getSyncState()
            val attemptAt = Clock.System.now().toEpochMilliseconds()
            try {
                if (!smsReader.hasPermission()) {
                    smsStore.updateSyncState(
                        syncState.copy(
                            lastAttemptEpochMs = attemptAt,
                            lastError = "Permission not granted",
                        ),
                    )
                    return@withLock
                }

                // First-time enable: seed lastSeenId to the current max and skip the read —
                // everything already in the inbox is "history" and shouldn't flood the
                // pending queue. Subsequent polls only pick up messages with _id > lastSeenId.
                if (syncState.lastSyncEpochMs == 0L) {
                    smsStore.updateSyncState(
                        syncState.copy(
                            lastSyncEpochMs = attemptAt,
                            lastAttemptEpochMs = attemptAt,
                            lastError = null,
                            lastSeenId = smsReader.currentMaxInboxId(),
                        ),
                    )
                    return@withLock
                }

                val eligibleMessages = smsReader.readInboxSince(syncState.lastSeenId, MAX_FETCH_PER_POLL)
                    .asSequence()
                    .filter { it.id > syncState.lastSeenId }
                    .distinctBy { it.id }
                    .take(MAX_FETCH_PER_POLL)
                    .toList()
                val pendingIds = smsStore.getPending().mapTo(mutableSetOf()) { it.id }
                val newMessages = eligibleMessages.filterNot { it.id in pendingIds }
                var updated = syncState.copy(
                    lastSyncEpochMs = attemptAt,
                    lastAttemptEpochMs = attemptAt,
                    unreadCount = newMessages.count { !it.read },
                    lastError = null,
                )
                if (newMessages.isNotEmpty()) {
                    smsStore.addPending(newMessages)
                }
                if (eligibleMessages.isNotEmpty()) {
                    updated = updated.copy(lastSeenId = eligibleMessages.maxOf { it.id })
                }
                smsStore.updateSyncState(updated)
            } catch (c: CancellationException) {
                throw c
            } catch (e: Exception) {
                smsStore.updateSyncState(
                    syncState.copy(
                        lastAttemptEpochMs = attemptAt,
                        lastError = e.message ?: e::class.simpleName ?: "Poll failed",
                    ),
                )
            }
        }
    }

    companion object {
        const val MAX_FETCH_PER_POLL = 50
    }
}
