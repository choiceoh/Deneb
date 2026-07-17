package ai.deneb.data

import kotlinx.coroutines.sync.Mutex

/** Decode persisted JSON, returning [defaultValue] for absent or malformed data. */
internal inline fun <T> decodeStoredJsonOrDefault(
    raw: String,
    defaultValue: () -> T,
    onMalformed: (Exception) -> Unit = {},
    decode: (String) -> T,
): T {
    if (raw.isEmpty()) return defaultValue()
    return try {
        decode(raw)
    } catch (error: Exception) {
        runCatching { onMalformed(error) }
        defaultValue()
    }
}

/**
 * Read persisted JSON and repair malformed storage only when this reader owns
 * [this] mutex. A reader racing with a mutation still returns a safe default,
 * but cannot clear the mutation's incoming write.
 */
internal inline fun <T> Mutex.loadStoredJsonOrDefault(
    readJson: () -> String,
    clearMalformed: () -> Unit,
    defaultValue: () -> T,
    onMalformed: (Exception) -> Unit = {},
    decode: (String) -> T,
): T {
    val canRepair = tryLock()
    return try {
        decodeStoredJsonOrDefault(
            raw = readJson(),
            defaultValue = defaultValue,
            onMalformed = { error ->
                runCatching { onMalformed(error) }
                if (canRepair) clearMalformed()
            },
            decode = decode,
        )
    } finally {
        if (canRepair) unlock()
    }
}
