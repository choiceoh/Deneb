package ai.deneb.data

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
    private val json = SharedJson
    private val document = StoredJsonDocument(
        readJson = readJson,
        writeJson = writeJson,
        defaultValue = { emptyList() },
        decode = { json.decodeFromString(serializer, it) },
        encode = { json.encodeToString(serializer, it) },
    )

    init {
        require(maxSize >= 0) { "maxSize must not be negative" }
    }

    fun get(): List<T> = document.read()

    suspend fun add(items: List<T>) {
        if (items.isEmpty()) return
        document.mutate { current ->
            val updated = (current + items).takeLast(maxSize)
            if (updated == current) keepStoredJson(Unit) else persistStoredJson(updated, Unit)
        }
    }

    suspend fun remove(items: List<T>) {
        if (items.isEmpty()) return
        val keys = items.map(keyOf).toSet()
        document.mutate { current ->
            val updated = current.filterNot { keyOf(it) in keys }
            if (updated == current) keepStoredJson(Unit) else persistStoredJson(updated, Unit)
        }
    }

    suspend fun clear() = document.clear()
}
