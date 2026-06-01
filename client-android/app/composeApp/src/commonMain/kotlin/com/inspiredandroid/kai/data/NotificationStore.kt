package com.inspiredandroid.kai.data

import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.serializer
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

/**
 * Persistence for notifications captured by [com.inspiredandroid.kai.notifications.KaiNotificationListenerService].
 *
 * Two collections:
 * - **Pending queue** — capped FIFO that fills as the listener fires and gets snapshotted
 *   into the heartbeat prompt, then drained. Mirrors [SmsStore].
 * - **Store** — broader rolling history backing [com.inspiredandroid.kai.notifications.NotificationReader],
 *   bounded by per-app cap and age cap.
 *
 * Per-app gating is handled by the system Notification Access "Apps" picker — if the
 * user unchecks an app there, `onNotificationPosted` is never called for that package
 * in the first place, so this store never sees it.
 */
@OptIn(ExperimentalTime::class)
class NotificationStore(private val appSettings: AppSettings) {

    private val json = SharedJson
    private val mutex = Mutex()
    private val pendingQueue = PendingQueue<NotificationRecord, String>(
        readJson = appSettings::getNotificationsPendingJson,
        writeJson = appSettings::setNotificationsPendingJson,
        serializer = ListSerializer(serializer<NotificationRecord>()),
        keyOf = { it.id },
    )

    /**
     * Fires once per newly captured notification so the scheduler can run a
     * heartbeat immediately instead of waiting for the next poll. Buffered +
     * DROP_OLDEST so a burst never blocks the listener; the scheduler debounces.
     */
    private val _captured = MutableSharedFlow<Unit>(
        extraBufferCapacity = 16,
        onBufferOverflow = kotlinx.coroutines.channels.BufferOverflow.DROP_OLDEST,
    )
    val captured: SharedFlow<Unit> = _captured

    // In-memory cache of captured notification images (BigPictureStyle
    // EXTRA_PICTURE), keyed by record id. Kept OUT of the persisted store so
    // base64 bitmaps don't bloat prefs; the listener fills it and the manual-
    // send tab drains it through the OCR capture path. Lost on process death
    // (then a record's hasImage just falls back to text-only). Bounded so a
    // burst of image notifications can't grow memory without limit.
    private val imageMutex = Mutex()
    private val images = LinkedHashMap<String, ByteArray>()

    suspend fun putImage(id: String, bytes: ByteArray) = imageMutex.withLock {
        if (bytes.isEmpty()) return@withLock
        images.remove(id) // re-insert so this id becomes the most-recent entry
        images[id] = bytes
        while (images.size > MAX_IMAGES) {
            val oldest = images.keys.firstOrNull() ?: break
            images.remove(oldest)
        }
    }

    suspend fun getImage(id: String): ByteArray? = imageMutex.withLock { images[id] }

    fun getPending(): List<NotificationRecord> = pendingQueue.get()

    suspend fun addPending(record: NotificationRecord) {
        pendingQueue.add(listOf(record))
        _captured.tryEmit(Unit)
    }

    suspend fun removePending(records: List<NotificationRecord>) = pendingQueue.remove(records)

    suspend fun clearPending() = pendingQueue.clear()

    fun getStore(): List<NotificationRecord> {
        val raw = appSettings.getNotificationsStoreJson()
        if (raw.isEmpty()) return emptyList()
        return try {
            json.decodeFromString<List<NotificationRecord>>(raw)
        } catch (_: Exception) {
            emptyList()
        }
    }

    suspend fun addRecord(record: NotificationRecord) = mutex.withLock {
        val now = Clock.System.now().toEpochMilliseconds()
        val ageCutoff = now - MAX_AGE_MS
        val current = getStore()
            .filter { it.postedAtEpochMs >= ageCutoff }
        // Per-package cap: keep newest [MAX_PER_PACKAGE] for each package after adding the new record.
        val updated = (current + record)
            .groupBy { it.packageName }
            .flatMap { (_, msgs) -> msgs.sortedByDescending { it.postedAtEpochMs }.take(MAX_PER_PACKAGE) }
            .sortedByDescending { it.postedAtEpochMs }
        appSettings.setNotificationsStoreJson(json.encodeToString(updated))
    }

    /** Drops records older than 24h or beyond the per-package cap. Called after each heartbeat. */
    suspend fun sweep() = mutex.withLock {
        val now = Clock.System.now().toEpochMilliseconds()
        val ageCutoff = now - MAX_AGE_MS
        val swept = getStore()
            .filter { it.postedAtEpochMs >= ageCutoff }
            .groupBy { it.packageName }
            .flatMap { (_, msgs) -> msgs.sortedByDescending { it.postedAtEpochMs }.take(MAX_PER_PACKAGE) }
            .sortedByDescending { it.postedAtEpochMs }
        appSettings.setNotificationsStoreJson(json.encodeToString(swept))
        // Drop cached images whose record was swept out, so the image cache
        // tracks the visible store rather than growing until the cap evicts.
        val liveIds = swept.mapTo(HashSet()) { it.id }
        imageMutex.withLock { images.keys.retainAll(liveIds) }
    }

    fun getSyncState(): NotificationSyncState {
        val raw = appSettings.getNotificationsSyncStateJson()
        if (raw.isEmpty()) return NotificationSyncState()
        return try {
            json.decodeFromString<NotificationSyncState>(raw)
        } catch (_: Exception) {
            NotificationSyncState()
        }
    }

    suspend fun updateSyncState(state: NotificationSyncState) = mutex.withLock {
        appSettings.setNotificationsSyncStateJson(json.encodeToString(state))
    }

    companion object {
        private const val MAX_PER_PACKAGE = 50
        private const val MAX_AGE_MS = 24L * 60L * 60L * 1000L
        // Cap on cached notification images (memory only). A small ceiling is
        // plenty — images are forwarded manually, soon after they arrive.
        private const val MAX_IMAGES = 30
    }
}
