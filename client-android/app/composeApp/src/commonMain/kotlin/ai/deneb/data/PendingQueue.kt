package ai.deneb.data

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.KSerializer

/**
 * Capped FIFO queue persisted as JSON via [readJson]/[writeJson]. Generic over the item type [T]
 * and a stable key type [K] used to identify items for removal. Shared by `EmailStore`
 * and `SmsStore` to enforce a uniform pending-buffer discipline.
 */
class PendingQueue<T, K>(
    private val readJson: () -> String,
    private val writeJson: (String) -> Unit,
    private val serializer: KSerializer<List<T>>,
    private val keyOf: (T) -> K,
    private val maxSize: Int = 100,
) {
    private val mutex = Mutex()
    private val json = SharedJson

    fun get(): List<T> {
        // Only the lock owner may repair malformed storage. A concurrent reader
        // still gets a safe snapshot but cannot clear a valid write racing in.
        if (!mutex.tryLock()) return readStored()
        return try {
            readStored(clearMalformed = true)
        } finally {
            mutex.unlock()
        }
    }

    private fun readStored(clearMalformed: Boolean = false): List<T> {
        val raw = readJson()
        if (raw.isEmpty()) return emptyList()
        return try {
            json.decodeFromString(serializer, raw)
        } catch (_: Exception) {
            if (clearMalformed) runCatching { writeJson("") }
            emptyList()
        }
    }

    suspend fun add(items: List<T>) = mutex.withLock {
        if (items.isEmpty()) return@withLock
        writeJson(json.encodeToString(serializer, (readStored() + items).takeLast(maxSize)))
    }

    suspend fun remove(items: List<T>) = mutex.withLock {
        if (items.isEmpty()) return@withLock
        val keys = items.map(keyOf).toSet()
        writeJson(json.encodeToString(serializer, readStored().filterNot { keyOf(it) in keys }))
    }

    suspend fun clear() = mutex.withLock {
        writeJson("")
    }
}
