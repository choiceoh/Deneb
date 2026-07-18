package ai.deneb.data

import ai.deneb.DenebLog
import androidx.compose.runtime.Immutable
import kotlinx.serialization.Serializable
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

@Serializable
enum class MemoryCategory {
    GENERAL,
    LEARNING,
    ERROR,
    PREFERENCE,
}

@Immutable
@Serializable
data class MemoryEntry(
    val key: String,
    val content: String,
    val createdAt: Long,
    val updatedAt: Long,
    val category: MemoryCategory = MemoryCategory.GENERAL,
    val hitCount: Int = 1,
    val source: String? = null,
)

@OptIn(ExperimentalTime::class)
class MemoryStore(private val appSettings: AppSettings) {

    private val json = SharedJson
    private val memories = StoredJsonDocument<List<MemoryEntry>>(
        readJson = appSettings::getMemoriesJson,
        writeJson = appSettings::setMemoriesJson,
        defaultValue = { emptyList() },
        onMalformed = { DenebLog.error("MemoryStore", "failed to load memories: ${it.message}") },
        decode = { json.decodeFromString<List<MemoryEntry>>(it) },
        encode = json::encodeToString,
    )

    suspend fun store(
        key: String,
        content: String,
        category: MemoryCategory = MemoryCategory.GENERAL,
        source: String? = null,
    ): MemoryEntry = memories.mutate { current ->
        val updatedMemories = current.toMutableList()
        val now = Clock.System.now().toEpochMilliseconds()
        val existing = updatedMemories.indexOfFirst { it.key == key }
        val entry = if (existing >= 0) {
            val updated = updatedMemories[existing].copy(
                content = content,
                updatedAt = now,
                category = category,
                source = source ?: updatedMemories[existing].source,
            )
            updatedMemories[existing] = updated
            updated
        } else {
            val newEntry = MemoryEntry(key = key, content = content, createdAt = now, updatedAt = now, category = category, source = source)
            updatedMemories.add(newEntry)
            newEntry
        }
        persistStoredJson(updatedMemories, entry)
    }

    suspend fun updateContent(key: String, content: String): MemoryEntry? = memories.mutate { current ->
        val index = current.indexOfFirst { it.key == key }
        if (index < 0) return@mutate keepStoredJson(null)
        val updatedMemories = current.toMutableList()
        val now = Clock.System.now().toEpochMilliseconds()
        val updated = updatedMemories[index].copy(content = content, updatedAt = now)
        updatedMemories[index] = updated
        persistStoredJson(updatedMemories, updated)
    }

    suspend fun reinforceMemory(key: String): MemoryEntry? = memories.mutate { current ->
        val index = current.indexOfFirst { it.key == key }
        if (index < 0) return@mutate keepStoredJson(null)
        val updatedMemories = current.toMutableList()
        val now = Clock.System.now().toEpochMilliseconds()
        val nextHitCount = if (updatedMemories[index].hitCount == Int.MAX_VALUE) Int.MAX_VALUE else updatedMemories[index].hitCount + 1
        val updated = updatedMemories[index].copy(hitCount = nextHitCount, updatedAt = now)
        updatedMemories[index] = updated
        persistStoredJson(updatedMemories, updated)
    }

    fun getPromotionCandidates(minHits: Int = 5): List<MemoryEntry> = memories.read().filter { it.hitCount >= minHits }

    suspend fun forget(key: String): Boolean = memories.mutate { current ->
        val updated = current.filterNot { it.key == key }
        if (updated.size == current.size) keepStoredJson(false) else persistStoredJson(updated, true)
    }

    fun getAllMemories(): List<MemoryEntry> = memories.read()
}
